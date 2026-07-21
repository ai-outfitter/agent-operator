{ pkgs, inputs, config, ... }:

let
  system = pkgs.stdenv.hostPlatform.system;
  root = config.devenv.root;
  clusterState = "${root}/.devenv/state/link-cluster";
  clusterShare = "${clusterState}/shared";
  kubeconfig = "${clusterShare}/kubeconfig";
  kubernetesPort = config.processes.cluster.ports.kubernetes.value;
  jmapPort = config.processes.cluster.ports.jmap.value;
  containersRegistries = pkgs.writeText "link-registries.conf" ''
    unqualified-search-registries = ["docker.io"]
  '';
  registryAuth = pkgs.writeText "link-registry-auth.json" "{}";

  clusterVm = inputs.nixos.lib.nixosSystem {
    inherit system;
    modules = [
      inputs.microvm.nixosModules.microvm
      ({ config, ... }: {
        system.stateVersion = "26.05";
        networking.hostName = "link-operator-dev";
        networking.useDHCP = false;
        systemd.network.enable = true;
        systemd.network.networks."20-wired" = {
          matchConfig.Name = "en* eth*";
          networkConfig.DHCP = "ipv4";
        };

        services.k3s = {
          enable = true;
          role = "server";
          disable = [ "servicelb" "traefik" ];
          extraFlags = toString [
            "--write-kubeconfig-mode=0600"
            "--tls-san=127.0.0.1"
          ];
        };

        networking.firewall.allowedTCPPorts = [ 6443 8080 ];

        systemd.services.link-kubeconfig = {
          description = "Publish the k3s kubeconfig to the development host";
          wantedBy = [ "multi-user.target" ];
          after = [ "k3s.service" "mnt-link\\x2dstate.mount" ];
          requires = [ "mnt-link\\x2dstate.mount" ];
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
          };
          path = [ pkgs.coreutils pkgs.gnused ];
          script = ''
            set -euo pipefail
            until [ -s /etc/rancher/k3s/k3s.yaml ]; do
              sleep 1
            done
            install -m 0600 /etc/rancher/k3s/k3s.yaml /mnt/link-state/kubeconfig
            sed -i 's#https://127.0.0.1:6443#https://127.0.0.1:${toString kubernetesPort}#' /mnt/link-state/kubeconfig
          '';
        };

        systemd.services.link-image-import = {
          description = "Import host-built development images into k3s";
          after = [ "k3s.service" "mnt-link\\x2dstate.mount" ];
          requires = [ "mnt-link\\x2dstate.mount" ];
          serviceConfig.Type = "oneshot";
          path = [ pkgs.coreutils pkgs.findutils pkgs.k3s ];
          script = ''
            set -euo pipefail
            mkdir -p /mnt/link-state/images /mnt/link-state/imported
            for archive in /mnt/link-state/images/*.tar; do
              [ -e "$archive" ] || continue
              name="$(basename "$archive")"
              digest="$(sha256sum "$archive" | cut -d' ' -f1)"
              stamp="/mnt/link-state/imported/$name.sha256"
              if [ -s "$stamp" ] && [ "$(tr -d '\n' < "$stamp")" = "$digest" ]; then
                continue
              fi
              k3s ctr images import "$archive"
              printf '%s\n' "$digest" > "$stamp"
            done
          '';
        };

        systemd.timers.link-image-import = {
          wantedBy = [ "timers.target" ];
          timerConfig = {
            OnBootSec = "5s";
            OnUnitActiveSec = "5s";
            Unit = "link-image-import.service";
          };
        };

        microvm = {
          hypervisor = "qemu";
          socket = "${clusterState}/control.socket";
          mem = 6144;
          vcpu = 4;
          interfaces = [{
            type = "user";
            id = "qemu";
            mac = "02:00:00:01:01:01";
          }];
          forwardPorts = [
            {
              from = "host";
              host.address = "127.0.0.1";
              host.port = kubernetesPort;
              guest.port = 6443;
            }
            {
              from = "host";
              host.address = "127.0.0.1";
              host.port = jmapPort;
              guest.port = 8081;
            }
          ];
          shares = [
            {
              proto = "9p";
              tag = "ro-store";
              source = "/nix/store";
              mountPoint = "/nix/.ro-store";
              readOnly = true;
            }
            {
              proto = "9p";
              tag = "link-state";
              source = clusterShare;
              mountPoint = "/mnt/link-state";
              readOnly = false;
            }
          ];
          volumes = [{
            image = "${clusterState}/k3s.img";
            label = "link-k3s";
            mountPoint = "/var/lib/rancher";
            size = 32768;
          }];
        };
      })
    ];
  };

  clusterRunner = clusterVm.config.microvm.declaredRunner;
in
{
  packages = with pkgs; [
    curl
    git
    go
    golangci-lint
    gnumake
    jq
    kubebuilder
    kubectl
    kustomize
    podman
    setup-envtest
    skopeo
    socat
    yq-go
  ];

  env = {
    CGO_ENABLED = "0";
    CONTAINERS_REGISTRIES_CONF = "${containersRegistries}";
    KUBECONFIG = kubeconfig;
    REGISTRY_AUTH_FILE = "${registryAuth}";
    SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";
  };

  process.manager.implementation = "native";
  processes.cluster = {
    ports.kubernetes.allocate = 6443;
    ports.jmap.allocate = 18080;
    exec = ''
      mkdir -p ${clusterShare}/images ${clusterShare}/imported
      exec ${clusterRunner}/bin/microvm-run
    '';
    restart.on = "on_failure";
  };

  tasks = {
    "operator:generate" = {
      cwd = "code/operator";
      exec = "exec make generate manifests";
    };

    "operator:fmt" = {
      cwd = "code/operator";
      after = [ "operator:generate" ];
      exec = "exec make fmt vet";
    };

    "operator:test" = {
      cwd = "code/operator";
      after = [ "operator:fmt" ];
      exec = "exec make test";
    };

    "operator:lint" = {
      cwd = "code/operator";
      after = [ "operator:test" ];
      exec = "exec make lint";
    };

    "operator:check" = {
      after = [ "operator:lint" ];
    };

    "agent:check" = {
      cwd = "code/agent";
      exec = ''
        set -euo pipefail
        unformatted="$(gofmt -l .)"
        if [ -n "$unformatted" ]; then
          echo "Go files need formatting:"
          echo "$unformatted"
          exit 1
        fi
        go vet ./...
        go test ./...
      '';
    };

    "agent:image" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        podman build --tag link-agent:dev --file code/agent/Dockerfile code/agent
        archive=${clusterShare}/images/link-agent-dev.tar
        temporary="$archive.tmp"
        mkdir -p ${clusterShare}/images ${clusterShare}/imported
        podman save --output "$temporary" link-agent:dev
        mv "$temporary" "$archive"
        digest="$(sha256sum "$archive" | cut -d' ' -f1)"
        stamp=${clusterShare}/imported/link-agent-dev.tar.sha256
        if kubectl get --raw=/readyz >/dev/null 2>&1; then
          until [ -s "$stamp" ] && [ "$(tr -d '\n' < "$stamp")" = "$digest" ]; do
            sleep 2
          done
        fi
        echo "agent image ready: localhost/link-agent:dev"
      '';
    };

    "operator:image" = {
      cwd = "code/operator";
      exec = ''
        : "''${IMG:=link-operator:dev}"
        exec make docker-build IMG="$IMG" CONTAINER_TOOL=podman
      '';
    };

    "cluster:up" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        devenv processes up -d cluster
        until kubectl get --raw=/readyz >/dev/null 2>&1; do
          sleep 2
        done
        kubectl apply -f dev/cluster/stalwart.yaml
        kubectl -n link-system wait \
          --for=jsonpath='{.status.phase}'=Running \
          pod --selector=app.kubernetes.io/name=stalwart \
          --timeout=5m
        kubectl -n link-system delete job stalwart-bootstrap --ignore-not-found --wait=true
        kubectl apply -f dev/cluster/stalwart-bootstrap.yaml
        if ! kubectl -n link-system wait --for=condition=complete job/stalwart-bootstrap --timeout=3m; then
          kubectl -n link-system logs job/stalwart-bootstrap --all-containers=true
          exit 1
        fi
        kubectl -n link-system rollout restart statefulset/stalwart
        kubectl -n link-system rollout status statefulset/stalwart --timeout=5m
        until curl --fail --silent \
          --user 'researcher@link.test:researcher-dev-password-2026!' \
          http://127.0.0.1:${toString jmapPort}/.well-known/jmap >/dev/null; do
          sleep 2
        done
        echo
        echo "link-operator cluster ready"
        echo "  kubeconfig  ${kubeconfig}"
        echo "  kubernetes  https://127.0.0.1:${toString kubernetesPort}"
        echo "  jmap        http://127.0.0.1:${toString jmapPort}"
        echo "  accounts    researcher@link.test / demo-user@link.test"
      '';
    };

    "operator:install" = {
      showOutput = true;
      after = [ "agent:image" ];
      exec = ''
        set -euo pipefail
        kubectl get --raw=/readyz >/dev/null
        make -C code/operator docker-build IMG=link-operator:dev CONTAINER_TOOL=podman
        archive=${clusterShare}/images/link-operator-dev.tar
        temporary="$archive.tmp"
        podman save --output "$temporary" link-operator:dev
        mv "$temporary" "$archive"
        digest="$(sha256sum "$archive" | cut -d' ' -f1)"
        stamp=${clusterShare}/imported/link-operator-dev.tar.sha256
        until [ -s "$stamp" ] && [ "$(tr -d '\n' < "$stamp")" = "$digest" ]; do
          sleep 2
        done
        kubectl apply -k code/operator/config/dev
        kubectl -n link-operator-system rollout restart deployment/link-operator-controller-manager
        kubectl -n link-operator-system rollout status deployment/link-operator-controller-manager --timeout=3m
        kubectl api-resources --api-group=link.aioutfitter.com
      '';
    };

    "agent:pi-sync" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        if [ ! -d "$HOME/.pi" ]; then
          echo "Local Pi configuration not found at $HOME/.pi" >&2
          exit 1
        fi
        kubectl -n agent-researcher delete pod pi-seeder --ignore-not-found --wait=true
        kubectl apply -f dev/demo/mail-loop/pi-seeder.yaml
        if ! kubectl -n agent-researcher wait --for=condition=Ready pod/pi-seeder --timeout=2m; then
          kubectl -n agent-researcher describe pod/pi-seeder
          exit 1
        fi
        tar -C "$HOME" -cf - .pi | kubectl -n agent-researcher exec -i pi-seeder -- tar -C /workspace -xf -
        kubectl -n agent-researcher exec pi-seeder -- test -d /workspace/.pi/agent
        if [ -f "$HOME/.pi/agent/auth.json" ]; then
          kubectl -n agent-researcher exec pi-seeder -- test -s /workspace/.pi/agent/auth.json
        fi
        kubectl -n agent-researcher delete pod pi-seeder --wait=true
        kubectl -n agent-researcher create configmap researcher-pi-ready \
          --from-literal=LINK_PI_CONFIG_READY=true \
          --dry-run=client -o yaml | kubectl apply -f -
        echo "copied $HOME/.pi into the researcher agent workspace volume"
      '';
    };

    "demo:mail-loop" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        kubectl get --raw=/readyz >/dev/null
        kubectl apply -f dev/demo/mail-loop/organization.yaml
        kubectl apply -f dev/demo/mail-loop/agent.yaml
        for attempt in $(seq 1 60); do
          if kubectl get namespace agent-researcher >/dev/null 2>&1; then
            break
          fi
          sleep 1
        done
        kubectl get namespace agent-researcher >/dev/null
        kubectl -n agent-researcher create secret generic researcher-email \
          --from-literal=JMAP_URL=http://stalwart.link-system.svc.cluster.local:8080 \
          --from-literal=JMAP_USERNAME=researcher@link.test \
          --from-literal=JMAP_PASSWORD='researcher-dev-password-2026!' \
          --dry-run=client -o yaml | kubectl apply -f -
        kubectl -n agent-researcher create configmap researcher-runtime \
          --from-literal=LINK_MAIL_POLL_INTERVAL=2s \
          --dry-run=client -o yaml | kubectl apply -f -
        devenv tasks run agent:pi-sync
        kubectl -n agent-researcher rollout restart deployment/agent-runtime
        kubectl -n agent-researcher rollout status deployment/agent-runtime --timeout=3m
        probe_token="$(date -u +%Y%m%dT%H%M%S)-$$"
        probe_subject="Link mail loop probe $probe_token"
        probe_message_id="link-mail-loop-$probe_token@link.test"
        evidence=${clusterShare}/evidence/mail-loop
        mkdir -p "$evidence"
        JMAP_URL=http://127.0.0.1:${toString jmapPort} \
          JMAP_USERNAME=demo-user@link.test \
          JMAP_PASSWORD='demo-user-dev-password-2026!' \
          go -C code/agent run ./cmd/link-agent send \
            --to researcher@link.test \
            --subject "$probe_subject" \
            --message-id "$probe_message_id"
        JMAP_URL=http://127.0.0.1:${toString jmapPort} \
          JMAP_USERNAME=demo-user@link.test \
          JMAP_PASSWORD='demo-user-dev-password-2026!' \
          go -C code/agent run ./cmd/link-agent wait-reply \
            --in-reply-to "$probe_message_id" \
            --return-address researcher@link.test \
            --to demo-user@link.test \
            --timeout 2m > "$evidence/reply.json"
        observed=false
        for attempt in $(seq 1 60); do
          if kubectl -n agent-researcher exec deployment/agent-runtime -- \
            link-agent state --has-subject "$probe_subject" >/dev/null 2>&1; then
            observed=true
            break
          fi
          sleep 2
        done
        if [ "$observed" != true ]; then
          kubectl -n agent-researcher logs deployment/agent-runtime --tail=200
          exit 1
        fi
        kubectl -n agent-researcher exec deployment/agent-runtime -- \
          test -s /workspace/.pi/agent/auth.json
        kubectl -n agent-researcher rollout restart deployment/agent-runtime
        kubectl -n agent-researcher rollout status deployment/agent-runtime --timeout=3m
        kubectl -n agent-researcher exec deployment/agent-runtime -- \
          link-agent state --has-subject "$probe_subject"
        JMAP_URL=http://127.0.0.1:${toString jmapPort} \
          JMAP_USERNAME=demo-user@link.test \
          JMAP_PASSWORD='demo-user-dev-password-2026!' \
          go -C code/agent run ./cmd/link-agent wait-reply \
            --in-reply-to "$probe_message_id" \
            --return-address researcher@link.test \
            --to demo-user@link.test \
            --timeout 10s > "$evidence/reply-after-restart.json"
        kubectl -n agent-researcher exec deployment/agent-runtime -- link-agent state > "$evidence/state.json"
        kubectl get agent researcher -o yaml > "$evidence/agent.yaml"
        echo "$probe_subject" > "$evidence/subject.txt"
        echo "$probe_message_id" > "$evidence/message-id.txt"
        echo "mail reply verified from researcher@link.test after restart: $probe_subject"
        echo "evidence: $evidence"
      '';
    };

    "cluster:down" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        control_socket=${clusterState}/control.socket
        if [ -S "$control_socket" ]; then
          {
            echo '{"execute":"qmp_capabilities"}'
            echo '{"execute":"system_powerdown"}'
          } | socat - UNIX-CONNECT:"$control_socket" >/dev/null
          for attempt in $(seq 1 30); do
            if [ ! -S "$control_socket" ]; then
              break
            fi
            sleep 1
          done
          if [ -S "$control_socket" ]; then
            echo "cluster did not stop within 30 seconds" >&2
            exit 1
          fi
        fi
        echo "cluster stopped; disk, image cache, and kubeconfig are preserved"
      '';
    };

    "cluster:reset-destructive" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        if [ "''${CONFIRM_LINK_CLUSTER_RESET:-}" != "destroy-link-cluster" ]; then
          echo "Refusing to delete ${clusterState}."
          echo "Re-run with CONFIRM_LINK_CLUSTER_RESET=destroy-link-cluster."
          exit 1
        fi
        devenv processes stop cluster || true
        rm -f ${clusterState}/k3s.img ${clusterState}/control.socket
        rm -rf ${clusterShare}
        echo "deleted the local cluster disk, shared image cache, and kubeconfig"
      '';
    };
  };

  enterShell = ''
    echo "link-operator devenv: go $(go version | cut -d' ' -f3), kubebuilder $(kubebuilder version 2>/dev/null | head -n1)"
    echo "  cluster: devenv tasks run cluster:up"
    echo "  checks:  devenv tasks run operator:check"
  '';
}
