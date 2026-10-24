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
runtime_settings = {}
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
    env = {e['name']: e for e in c['env']}
    mode = env['VOICE_INFERENCE_MODE']['value']
    assert mode in ('env', 'gateway')
    backend = env['VOICE_BACKEND']['value']
    assert backend in ('openai', 'gemini')
    assert env['VOICE_MODEL']['value']
    runtime_settings[role] = (backend, env['VOICE_MODEL']['value'], env.get('VOICE_VOICE', {}).get('value'))
    if mode == 'gateway':
        assert backend == 'openai', 'Gateway voice mode supports only OpenAI'

    if mode == 'gateway':
        # The base image predates mode support and would silently use env behavior.
        assert 'sha256:b5b1e369e1aac7e71d9907a00ba8ab8276b1ca4910bd28858ca9cbeb5abd8ef1' not in c['image']
    assert ('voice-provider' in secret_names) == (role == 'worker' and mode == 'env')
    if role == 'worker' and mode == 'gateway':
        assert 'VOICE_GATEWAY_URL' in env
        assert not any(k in env for k in ('OPENAI_API_KEY', 'GOOGLE_API_KEY', 'VOICE_SERVICE_TOKEN', 'VOICE_AUTHORITY_TOKEN'))
    if role == 'broker':
        assert env['VOICE_MODEL']['value']
        if mode == 'gateway':
            assert env['VOICE_AUTHORITY_TOKEN']['valueFrom']['secretKeyRef'] == {'name': 'voice-broker', 'key': 'authorityToken'}
        else:
            assert 'VOICE_AUTHORITY_TOKEN' not in env
    assert ('voice-broker' in secret_names) == (role == 'broker')
    assert 'voice-media' in secret_names
assert runtime_settings['broker'] == runtime_settings['worker'], 'Broker and worker backend/model/default voice must agree'
if ('Deployment', 'voice-inference-gateway') in objects:
    gateway = objects['Deployment', 'voice-inference-gateway']
    assert gateway['spec']['replicas'] == 1
    assert gateway['spec']['strategy']['type'] == 'Recreate'
    pod = gateway['spec']['template']['spec']
    assert pod['automountServiceAccountToken'] is False
    c = pod['containers'][0]
    assert c['command'] == ['node', 'workspace/gateway.mjs']
    assert c['image'] != objects['Deployment', 'voice-worker']['spec']['template']['spec']['containers'][0]['image'], 'Gateway and voice runtime require separate images'
    env = {e['name']: e for e in c['env']}
    assert env['WORKSPACE_DATABASE_URL']['value'] == 'file:/data/voice-gateway.db'
    assert env['VOICE_AUTHORITY_TOKEN']['valueFrom']['secretKeyRef'] == {'name': 'voice-broker', 'key': 'authorityToken'}
    assert env['WORKSPACE_GATEWAY_TOKEN']['valueFrom']['secretKeyRef']['name'] == 'voice-gateway-platform'
    assert not any(k.startswith(('WORKFLOW_', 'OPENAI_')) or k == 'INFERENCE_PROVIDERS' for k in env)
    assert objects['PersistentVolumeClaim', 'voice-inference-gateway-data']['spec']['accessModes'] == ['ReadWriteOnce']
pvc = objects['PersistentVolumeClaim', 'voice-broker-data']
assert pvc['spec']['accessModes'] == ['ReadWriteOnce']
ingress = objects['Ingress', 'voice-preview']
paths = ingress['spec']['rules'][0]['http']['paths']
assert {x['path'] for x in paths} == {'/residents', '/sessions', '/end'}
assert all(x['pathType'] == 'Exact' for x in paths)
assert all(x['metadata'].get('namespace') == 'voice-preview' for x in items if x['kind'] != 'Namespace')
print('PASS: isolated namespace, singleton SQLite, credential separation, exact public routes')
