#!/usr/bin/env python3
"""Check kubectl create --dry-run=client -k nonprod --validate=false -o json."""
import json
import sys

# kubectl emits consecutive JSON documents, not a List, for multiple resources.
source = sys.stdin.read().strip()
items = []
while source:
    item, end = json.JSONDecoder().raw_decode(source)
    items.append(item)
    source = source[end:].lstrip()
assert not any(x['kind'] in ('Secret', 'CustomResourceDefinition', 'ClusterRole') for x in items)
objects = {(x['kind'], x['metadata']['name']): x for x in items}
for role in ('broker', 'worker'):
    d = objects['Deployment', 'voice-' + role]
    assert d['spec']['replicas'] == 1
    assert d['spec']['strategy']['type'] == 'Recreate'
    spec = d['spec']['template']['spec']
    assert spec['automountServiceAccountToken'] is False
    assert spec['securityContext']['runAsUser'] == 1000
    c = spec['containers'][0]
    assert c['securityContext']['readOnlyRootFilesystem'] is True
    secret_names = {e.get('valueFrom', {}).get('secretKeyRef', {}).get('name') for e in c['env']}
    assert ('voice-provider' in secret_names) == (role == 'worker')
    assert ('voice-broker' in secret_names) == (role == 'broker')
    assert 'voice-media' in secret_names
pvc = objects['PersistentVolumeClaim', 'voice-broker-data']
assert pvc['spec']['accessModes'] == ['ReadWriteOnce']
ingress = objects['Ingress', 'voice-preview']
paths = ingress['spec']['rules'][0]['http']['paths']
assert {x['path'] for x in paths} == {'/residents', '/sessions', '/end'}
assert all(x['pathType'] == 'Exact' for x in paths)
assert all(x['metadata'].get('namespace') == 'voice-preview' for x in items if x['kind'] != 'Namespace')
print('PASS: isolated namespace, singleton SQLite, credential separation, exact public routes')
