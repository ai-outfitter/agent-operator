#!/usr/bin/env python3
"""Review/create bot voice resources, binding ownership to the verified live Agent."""
import argparse
import json
from pathlib import Path
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--apply', action='store_true', help='Create reviewed missing resources; default only prints JSON.')
args = parser.parse_args()
context = 'webapp-nonprod'
namespace = 'agent-unsupervised-nonprod-bot'
agent_name = 'unsupervised-nonprod-bot'


def kubectl(*arguments, payload=None):
    return subprocess.check_output(
        ['kubectl', '--context', context, *arguments],
        input=None if payload is None else json.dumps(payload).encode(),
    ).decode()


agent = json.loads(kubectl('get', 'agent', agent_name, '-o', 'json'))
ns = json.loads(kubectl('get', 'namespace', namespace, '-o', 'json'))
uid = agent['metadata']['uid']
if agent.get('status', {}).get('namespace') != namespace or ns['metadata'].get('labels', {}).get('aioutfitter.com/agent-uid') != uid:
    raise SystemExit('Agent namespace/UID ownership mismatch; refusing bootstrap.')
if agent['metadata'].get('deletionTimestamp') or ns['metadata'].get('deletionTimestamp'):
    raise SystemExit('Agent or namespace is deleting; refusing bootstrap.')
source = kubectl('create', '--dry-run=client', '--validate=false', '-f', str(Path(__file__).with_name('bot-a2a.yaml')), '-o', 'json').strip()
objects = []
while source:
    value, end = json.JSONDecoder().raw_decode(source)
    objects.extend(value['items'] if value.get('kind') == 'List' else [value])
    source = source[end:].lstrip()
owner = {'apiVersion': agent['apiVersion'], 'kind': 'Agent', 'name': agent_name, 'uid': uid, 'controller': True, 'blockOwnerDeletion': True}
missing = []
for obj in objects:
    if obj['metadata']['namespace'] != namespace or obj['kind'] not in ('ConfigMap', 'Service'):
        raise SystemExit('Unexpected bootstrap resource.')
    obj['metadata']['ownerReferences'] = [owner]
    existing_text = kubectl('get', obj['kind'], obj['metadata']['name'], '-n', namespace, '--ignore-not-found', '-o', 'json')
    if existing_text.strip():
        existing = json.loads(existing_text)
        if owner not in existing['metadata'].get('ownerReferences', []):
            raise SystemExit('Existing resource has different ownership; refusing adoption.')
        wanted = obj.get('data') if obj['kind'] == 'ConfigMap' else obj['spec']
        actual = existing.get('data') if obj['kind'] == 'ConfigMap' else {key: existing['spec'].get(key) for key in wanted}
        if wanted != actual:
            raise SystemExit('Existing resource differs; review an explicit update separately.')
    else:
        missing.append(obj)
plan = {'apiVersion': 'v1', 'kind': 'List', 'items': missing}
print(json.dumps(plan, indent=2))
if args.apply and missing:
    # Create rather than apply: a concurrent creator fails safely instead of being adopted.
    kubectl('create', '--dry-run=server', '-f', '-', payload=plan)
    fresh = json.loads(kubectl('get', 'agent', agent_name, '-o', 'json'))
    if fresh['metadata']['uid'] != uid or fresh['metadata'].get('deletionTimestamp'):
        raise SystemExit('Agent identity changed before creation; refusing bootstrap.')
    print(kubectl('create', '-f', '-', payload=plan))
