# Operating context

You run in an ephemeral, egress-restricted container. Your home is `/workspace`
(a persistent volume); a persistent Nix store is mounted at `/nix`, so anything
you `nix profile install` survives restarts. Mail is reached through the `xin`
JMAP CLI (credentials are already in the environment).

<!-- TODO: these agent and skill resources are currently baked into the
     agent image under /opt/agent/.agents. They should move to the Organization's
     remote Outfitter catalog (rendered into settings.yml `sources` by the
     operator) once runtime egress to the catalog is available. -->
