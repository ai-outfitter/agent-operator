#!/usr/bin/env python3
"""Check rendered public overlay as consecutive JSON documents (yq -o=json)."""
import json
import sys

source = sys.stdin.read().strip()
items = []
while source:
    item, end = json.JSONDecoder().raw_decode(source)
    items.append(item)
    source = source[end:].lstrip()
objects = {(x['kind'], x['metadata']['name']): x for x in items}
assert not any(x['kind'] in ('Secret', 'CustomResourceDefinition', 'ClusterRole') for x in items)
ns = objects['Namespace', 'voice-media-preview']
assert ns['metadata']['labels']['pod-security.kubernetes.io/enforce'] == 'privileged'
d = objects['Deployment', 'voice-livekit']
assert d['spec']['replicas'] == 1
assert d['spec']['strategy']['type'] == 'Recreate'
pod = d['spec']['template']['spec']
assert pod['hostNetwork'] is True
assert pod['dnsPolicy'] == 'Default'
assert pod['securityContext']['runAsUser'] == 0
assert pod['securityContext']['runAsNonRoot'] is False
assert pod['nodeSelector'] == {'aioutfitter.com/voice-media': 'public'}
assert pod['tolerations'] == [{'key': 'aioutfitter.com/voice-media', 'operator': 'Equal', 'value': 'public', 'effect': 'NoSchedule'}]
assert pod['automountServiceAccountToken'] is False
assert pod['containers'][0]['securityContext']['capabilities'] == {'add': ['NET_BIND_SERVICE'], 'drop': ['ALL']}
assert pod['containers'][0]['securityContext']['allowPrivilegeEscalation'] is False
ports = {p['name']: p for p in pod['containers'][0]['ports']}
assert ports['turn-tls']['hostPort'] == 443
assert ports['turn-udp']['hostPort'] == 3478
assert ports['turn-udp']['protocol'] == 'UDP'
assert any(v.get('secret', {}).get('secretName') == 'voice-turn-tls' for v in pod['volumes'])
config = objects['ConfigMap', 'voice-livekit']['data']['livekit.yaml']
for setting in ('use_external_ip: true', 'udp_port: 7882', 'tcp_port: 7881', 'tls_port: 443', 'udp_port: 3478', 'cert_file: /etc/turn/tls.crt'):
    assert setting in config
assert objects['Service', 'voice-livekit']['spec'].get('type', 'ClusterIP') == 'ClusterIP'
assert all(x['metadata'].get('namespace') == 'voice-media-preview' for x in items if x['kind'] != 'Namespace')

ingress = objects['Ingress', 'voice-livekit']['spec']
assert ingress['ingressClassName'] == 'nginx'
assert ingress['rules'][0]['host'] == 'livekit-voice-preview.unsupervised.wtf'
assert ingress['tls'] == [{'hosts': ['livekit-voice-preview.unsupervised.wtf'], 'secretName': 'voice-livekit-tls'}]
for name, host, secret in [('voice-turn', 'turn-voice-preview.unsupervised.wtf', 'voice-turn-tls'), ('voice-livekit', 'livekit-voice-preview.unsupervised.wtf', 'voice-livekit-tls')]:
    cert = objects['Certificate', name]['spec']
    assert cert['dnsNames'] == [host]
    assert cert['secretName'] == secret
    assert cert['issuerRef'] == {'name': 'letsencrypt-cloudflare-dns', 'kind': 'ClusterIssuer'}

print('PASS: singleton public-node host networking, isolated namespace, TURN certificate and direct ICE config')
