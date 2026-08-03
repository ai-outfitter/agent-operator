{ pkgs, inputs, config, lib, ... }:

let
  system = pkgs.stdenv.hostPlatform.system;
  root = config.devenv.root;
  clusterState = "${root}/.devenv/state/agent-cluster";
  clusterShare = "${clusterState}/shared";
  kubeconfig = "${clusterShare}/kubeconfig";
  kubernetesPort = config.processes.cluster.ports.kubernetes.value;
  jmapPort = config.processes.cluster.ports.jmap.value;
  containersRegistries = pkgs.writeText "agent-registries.conf" ''
    unqualified-search-registries = ["docker.io"]
  '';
  registryAuth = pkgs.writeText "agent-registry-auth.json" "{}";
  outfitter = inputs.outfitter.packages.${system}.outfitter;
  xin = pkgs.callPackage ./nix/xin.nix { };
  channels = pkgs.callPackage ./nix/channels.nix { };
  chrome-devtools-mcp = pkgs.callPackage ./nix/chrome-devtools-mcp.nix { };
  operator = pkgs.callPackage ./nix/operator.nix { };

  emptyContainerHome = pkgs.runCommand "agent-container-empty-home" { } ''
    mkdir -p "$out"
  '';

  agentContainerRoot = pkgs.runCommand "agent-runtime-container-root" { } ''
    mkdir -p \
      "$out/opt/agent/.agents" \
      "$out/opt/agent/.cache" \
      "$out/workspace/.agent" \
      "$out/workspace/.pi/agent"
    cp -r ${./code/agent/agents-catalog}/. "$out/opt/agent/.agents/"
    cp -r ${channels}/. "$out/opt/agent/.cache/"
    cp ${./code/agent/entrypoint.sh} "$out/opt/agent/entrypoint"
    chmod +x "$out/opt/agent/entrypoint"
  '';

  agentContainerPackages = pkgs.buildEnv {
    name = "agent-runtime-container-packages";
    paths = [
      xin
      outfitter
      chrome-devtools-mcp
      pkgs.nodejs_22
      pkgs.jq
      pkgs.bash
      pkgs.coreutils
      pkgs.gitMinimal
      pkgs.gnutar
      pkgs.cacert
      pkgs.nix
      # Forge CLIs: agents authenticate from deployment-provided env
      # (GITHUB_TOKEN/GH_TOKEN for gh) or a profile/setup-written config
      # (tea, fj). Deployments can override the agent image entirely, so
      # this is the convenient default set, not a contract.
      pkgs.gh
      pkgs.tea
      pkgs.forgejo-cli
      pkgs.dockerTools.usrBinEnv
      pkgs.dockerTools.binSh
    ];
    pathsToLink = [ "/bin" "/etc" "/usr/bin" ];
    ignoreCollisions = true;
  };

  operatorContainerRoot = pkgs.buildEnv {
    name = "agent-operator-container-root";
    paths = [ operator pkgs.cacert ];
    pathsToLink = [ "/bin" "/etc" ];
  };

  clusterVm = inputs.nixos.lib.nixosSystem {
    inherit system;
    modules = [
      inputs.microvm.nixosModules.microvm
      ({ config, ... }: {
        system.stateVersion = "26.05";
        networking.hostName = "agent-operator-dev";
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

        systemd.services.agent-kubeconfig = {
          description = "Publish the k3s kubeconfig to the development host";
          wantedBy = [ "multi-user.target" ];
          after = [ "k3s.service" "mnt-agent\\x2dstate.mount" ];
          requires = [ "mnt-agent\\x2dstate.mount" ];
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
            install -m 0600 /etc/rancher/k3s/k3s.yaml /mnt/agent-state/kubeconfig
            sed -i 's#https://127.0.0.1:6443#https://127.0.0.1:${toString kubernetesPort}#' /mnt/agent-state/kubeconfig
          '';
        };

        systemd.services.agent-image-import = {
          description = "Import host-built development images into k3s";
          after = [ "k3s.service" "mnt-agent\\x2dstate.mount" ];
          requires = [ "mnt-agent\\x2dstate.mount" ];
          serviceConfig.Type = "oneshot";
          path = [ pkgs.coreutils pkgs.findutils pkgs.k3s ];
          script = ''
            set -euo pipefail
            mkdir -p /mnt/agent-state/images /mnt/agent-state/imported
            for archive in /mnt/agent-state/images/*.tar; do
              [ -e "$archive" ] || continue
              name="$(basename "$archive")"
              digest="$(sha256sum "$archive" | cut -d' ' -f1)"
              stamp="/mnt/agent-state/imported/$name.sha256"
              if [ -s "$stamp" ] && [ "$(tr -d '\n' < "$stamp")" = "$digest" ]; then
                continue
              fi
              k3s ctr images import "$archive"
              printf '%s\n' "$digest" > "$stamp"
            done
          '';
        };

        systemd.timers.agent-image-import = {
          wantedBy = [ "timers.target" ];
          timerConfig = {
            OnBootSec = "5s";
            OnUnitActiveSec = "5s";
            Unit = "agent-image-import.service";
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
              tag = "agent-state";
              source = clusterShare;
              mountPoint = "/mnt/agent-state";
              readOnly = false;
            }
          ];
          volumes = [{
            image = "${clusterState}/k3s.img";
            label = "agent-k3s";
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
  packages = lib.optionals (!config.container.isBuilding) (with pkgs; [
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
  ]);

  env = {
    CGO_ENABLED = "0";
    CONTAINERS_REGISTRIES_CONF = "${containersRegistries}";
    KUBECONFIG = kubeconfig;
    REGISTRY_AUTH_FILE = "${registryAuth}";
    SSL_CERT_FILE = "${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt";
  };

  # Development OCI images are assembled by devenv/nix2container. Component
  # packages remain flake outputs; this layer only describes their runtime
  # filesystem and entrypoint.
  containers.agent = {
    name = "localhost/agent-runtime";
    version = "dev";
    copyToRoot = emptyContainerHome;
    entrypoint = [ "/opt/agent/entrypoint" ];
    startupCommand = [ ];
    workingDir = "/workspace";
    maxLayers = 20;
    layers = [
      {
        # Keep the large, stable runtime closure reusable when only a skill,
        # loadout, or the entrypoint changes.
        copyToRoot = [ agentContainerPackages ];
      }
      {
        copyToRoot = [ agentContainerRoot ];
        perms = [{
          path = agentContainerRoot;
          regex = "/workspace(/.*)?";
          mode = "0755";
          uid = 1000;
          gid = 1000;
          uname = "user";
          gname = "user";
        }];
      }
    ];
  };

  containers.operator = {
    name = "localhost/agent-operator";
    version = "dev";
    copyToRoot = emptyContainerHome;
    entrypoint = [ "/bin/manager" ];
    startupCommand = [ ];
    workingDir = "/env";
    maxLayers = 20;
    layers = [{
      copyToRoot = [ operatorContainerRoot ];
    }];
  };

  process.manager.implementation = "native";
  processes.cluster = lib.mkIf (!config.container.isBuilding) {
    ports.kubernetes.allocate = 6443;
    ports.jmap.allocate = 18080;
    exec = ''
      mkdir -p ${clusterShare}/images ${clusterShare}/imported
      exec ${clusterRunner}/bin/microvm-run
    '';
    restart.on = "on_failure";
  };

  tasks = lib.mkIf (!config.container.isBuilding) {
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
      showOutput = true;
      exec = ''
        set -euo pipefail
        # The agent is a shell entrypoint plus a pinned, prebuilt Channels
        # extension. Agent profiles select channels and own their behavior.
        bash -n entrypoint.sh
        test -f agents-catalog/skills/mail/SKILL.md
        test ! -d agents-catalog/extensions
        grep -Fq 'outfitter run --strict "''${AGENT_SLUG:-researcher}" --' entrypoint.sh
        grep -Fq 'export PI_OFFLINE=1' entrypoint.sh
        grep -Fq 'PI_CODING_AGENT_SESSION_DIR' entrypoint.sh
        grep -Fq -- '--continue' entrypoint.sh
        if grep -Fq -- '--no-session' entrypoint.sh; then
          echo "resident agent entrypoint must persist its Pi session" >&2
          exit 1
        fi
        channels_dir=${channels}/outfitter/pi-extensions/git/github.com/ai-outfitter/channels
        test "$(tr -d '\n' < "$channels_dir/REVISION")" = "cac964724f149208a4d0fe2aca39e3e0a234045d"
        test "$(jq -r .name "$channels_dir/package.json")" = "@ai-outfitter/channels"
        validation_home="$(mktemp -d)"
        trap 'rm -rf "$validation_home"' EXIT
        (
          cd ${agentContainerRoot}/opt/agent
          HOME="$validation_home" \
            XDG_CACHE_HOME=${agentContainerRoot}/opt/agent/.cache \
            PI_OFFLINE=1 \
            ${outfitter}/bin/outfitter validate --strict
        )
        printf '%s\n' '{"type":"get_commands"}' | (
          cd ${agentContainerRoot}/opt/agent
          HOME="$validation_home" \
            XDG_CACHE_HOME=${agentContainerRoot}/opt/agent/.cache \
            PI_OFFLINE=1 \
            ${outfitter}/bin/outfitter run --strict researcher -- \
              --mode rpc --no-session --offline
        ) >"$validation_home/runtime.jsonl" 2>&1
        if ! grep -Fq '[channels] no channels started' "$validation_home/runtime.jsonl"; then
          cat "$validation_home/runtime.jsonl" >&2
          exit 1
        fi
      '';
    };

    "agent:image" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        bash dev/scripts/build-image.sh agent \
          ${clusterShare}/images/agent-runtime-dev.tar \
          ${clusterShare}/imported/agent-runtime-dev.tar.sha256
        echo "agent image ready: localhost/agent-runtime:dev"
      '';
    };

    "operator:image" = {
      showOutput = true;
      exec = ''
        set -euo pipefail
        bash dev/scripts/build-image.sh operator \
          ${clusterShare}/images/agent-operator-dev.tar \
          ${clusterShare}/imported/agent-operator-dev.tar.sha256
        echo "operator image ready: localhost/agent-operator:dev"
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
        kubectl -n agent-system wait \
          --for=jsonpath='{.status.phase}'=Running \
          pod --selector=app.kubernetes.io/name=stalwart \
          --timeout=5m
        kubectl -n agent-system delete job stalwart-bootstrap --ignore-not-found --wait=true
        kubectl apply -f dev/cluster/stalwart-bootstrap.yaml
        if ! kubectl -n agent-system wait --for=condition=complete job/stalwart-bootstrap --timeout=3m; then
          kubectl -n agent-system logs job/stalwart-bootstrap --all-containers=true
          exit 1
        fi
        kubectl -n agent-system rollout restart statefulset/stalwart
        kubectl -n agent-system rollout status statefulset/stalwart --timeout=5m
        until curl --fail --silent \
          --user 'researcher@outfitter.test:researcher-dev-password-2026!' \
          http://127.0.0.1:${toString jmapPort}/.well-known/jmap >/dev/null; do
          sleep 2
        done
        echo
        echo "agent-operator cluster ready"
        echo "  kubeconfig  ${kubeconfig}"
        echo "  kubernetes  https://127.0.0.1:${toString kubernetesPort}"
        echo "  jmap        http://127.0.0.1:${toString jmapPort}"
        echo "  accounts    researcher@outfitter.test / demo-user@outfitter.test"
      '';
    };

    "operator:install" = {
      showOutput = true;
      after = [ "agent:image" "operator:image" ];
      exec = ''
        set -euo pipefail
        kubectl get --raw=/readyz >/dev/null
        kubectl apply -k code/operator/config/dev
        kubectl -n agent-operator-system rollout restart deployment/agent-operator-controller-manager
        kubectl -n agent-operator-system rollout status deployment/agent-operator-controller-manager --timeout=3m
        kubectl api-resources --api-group=aioutfitter.com
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
          --from-literal=AGENT_PI_CONFIG_READY=true \
          --dry-run=client -o yaml | kubectl apply -f -
        echo "copied $HOME/.pi into the researcher agent workspace volume"
      '';
    };

    "demo:m1" = {
      showOutput = true;
      exec = ''
        set -euo pipefail

        probe_token="probe$(date -u +%Y%m%d%H%M%S)$$"
        probe_subject="Link M1 email probe $probe_token"
        evidence=${clusterShare}/evidence/m1-email-flow/$probe_token
        mkdir -p "$evidence"

        diagnose_m1() {
          exit_code=$?
          trap - EXIT
          if [ "$exit_code" -ne 0 ]; then
            set +e
            echo "demo:m1 failed; collecting diagnostics in $evidence" >&2
            kubectl get agent researcher -o yaml > "$evidence/agent.yaml" 2>&1
            kubectl -n agent-researcher get deployment,pods,pvc,events -o wide \
              > "$evidence/workload.txt" 2>&1
            kubectl -n agent-researcher describe deployment/agent-runtime \
              > "$evidence/deployment.txt" 2>&1
            pod="$(kubectl -n agent-researcher get pods \
              -l app.kubernetes.io/name=agent-runtime \
              -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)"
            if [ -n "$pod" ]; then
              kubectl -n agent-researcher describe pod "$pod" \
                > "$evidence/pod.txt" 2>&1
              kubectl -n agent-researcher logs "$pod" -c agent --tail=300 \
                > "$evidence/agent.log" 2>&1
              for init_container in seed-nix-store setup-mail-bootstrap; do
                kubectl -n agent-researcher logs "$pod" -c "$init_container" --tail=300 \
                  > "$evidence/$init_container.log" 2>&1
              done
            fi
          fi
          exit "$exit_code"
        }
        trap diagnose_m1 EXIT

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
          --from-literal=XIN_BASE_URL=http://stalwart.agent-system.svc.cluster.local:8080 \
          --from-literal=XIN_BASIC_USER=researcher@outfitter.test \
          --from-literal=XIN_BASIC_PASS='researcher-dev-password-2026!' \
          --dry-run=client -o yaml | kubectl apply -f -
        kubectl -n agent-researcher create configmap researcher-runtime \
          --from-literal=OUTFITTER_CHANNELS=jmap \
          --from-literal=AGENT_MAIL_PROCESSED=Processed \
          --dry-run=client -o yaml | kubectl apply -f -
        devenv tasks run agent:pi-sync
        kubectl -n agent-researcher rollout restart deployment/agent-runtime
        kubectl -n agent-researcher rollout status deployment/agent-runtime --timeout=3m

        # Wait for the inference-free JMAP stream before sending the probe. The
        # first model turn must be caused by the subsequent mailbox state event.
        channel_ready=0
        for attempt in $(seq 1 150); do
          if kubectl -n agent-researcher logs deployment/agent-runtime -c agent \
            | grep -Fq '[channels] started channel "jmap"'; then
            channel_ready=1
            break
          fi
          sleep 2
        done
        test "$channel_ready" -eq 1
        kubectl -n agent-researcher logs deployment/agent-runtime -c agent \
          > "$evidence/channel-ready.jsonl"

        stalwart_url=http://stalwart.agent-system.svc.cluster.local:8080
        # Drive the `xin` CLI inside the agent pod (it ships xin) as either
        # mailbox account by overriding the XIN_* env for that one exec.
        demo_user_xin() {
          kubectl -n agent-researcher exec deployment/agent-runtime -- \
            env XIN_BASE_URL="$stalwart_url" \
                XIN_BASIC_USER=demo-user@outfitter.test \
                XIN_BASIC_PASS='demo-user-dev-password-2026!' \
            xin "$@"
        }
        researcher_xin() {
          kubectl -n agent-researcher exec deployment/agent-runtime -- \
            env XIN_BASE_URL="$stalwart_url" \
                XIN_BASIC_USER=researcher@outfitter.test \
                XIN_BASIC_PASS='researcher-dev-password-2026!' \
            xin "$@"
        }

        # 1) demo-user sends a unique probe and records its generated Message-ID.
        demo_user_xin send --to researcher@outfitter.test \
          --subject "$probe_subject" \
          --text "Please process this M1 probe and reply." \
          > "$evidence/send.json"
        jq -e '.ok == true' "$evidence/send.json" >/dev/null
        sent_email_id="$(jq -er '.data.draft.emailId' "$evidence/send.json")"
        demo_user_xin get "$sent_email_id" \
          --headers message-id,from,to,subject > "$evidence/original-sent.json"
        probe_message_id="$(jq -er '.data.email.headers["message-id"]' \
          "$evidence/original-sent.json")"
        printf '%s\n' "$probe_message_id" > "$evidence/probe-message-id.txt"
        printf '%s\n' "$probe_subject" > "$evidence/subject.txt"

        # Capture recipient-side state before the JMAP event wakes the agent.
        researcher_xin messages search "in:inbox subject:$probe_token" --max 10 \
          > "$evidence/inbox-before.json"

        # 2) Wait for exactly one researcher reply in the demo-user INBOX.
        reply_count=0
        for attempt in $(seq 1 60); do
          demo_user_xin messages search "in:inbox subject:$probe_token" --max 10 \
            > "$evidence/reply-search.json"
          reply_count="$(jq '[.data.items[] | select(
            .from == [{"email":"researcher@outfitter.test","name":"M1 researcher agent"}] and
            .to == [{"email":"demo-user@outfitter.test","name":null}]
          )] | length' "$evidence/reply-search.json")"
          if [ "$reply_count" -eq 1 ]; then break; fi
          sleep 3
        done
        test "$reply_count" -eq 1
        reply_email_id="$(jq -er '.data.items[] | select(
          .from == [{"email":"researcher@outfitter.test","name":"M1 researcher agent"}] and
          .to == [{"email":"demo-user@outfitter.test","name":null}]
        ) | .emailId' "$evidence/reply-search.json")"
        demo_user_xin get "$reply_email_id" \
          --headers message-id,in-reply-to,references,from,to,subject \
          > "$evidence/reply.json"
        jq -e \
          --arg subject "Re: $probe_subject" \
          --arg message_id "$probe_message_id" '
            .ok == true and
            .data.email.headers.from == [{"email":"researcher@outfitter.test","name":"M1 researcher agent"}] and
            .data.email.headers.to == [{"email":"demo-user@outfitter.test","name":null}] and
            .data.email.headers.subject == $subject and
            .data.email.headers["in-reply-to"] == $message_id and
            .data.email.headers.references == [$message_id] and
            (.data.email.headers["message-id"] | type == "string" and length > 0)
          ' "$evidence/reply.json" >/dev/null

        # 3) Server-side state: exactly one original left INBOX for Processed.
        processed_count=0
        inbox_count=1
        for attempt in $(seq 1 30); do
          researcher_xin messages search "in:Processed subject:$probe_token" --max 10 \
            > "$evidence/processed-after.json"
          researcher_xin messages search "in:inbox subject:$probe_token" --max 10 \
            > "$evidence/inbox-after.json"
          processed_count="$(jq --arg subject "$probe_subject" '[.data.items[] | select(
            .subject == $subject and
            .from == [{"email":"demo-user@outfitter.test","name":"M1 demo sender"}] and
            .to == [{"email":"researcher@outfitter.test","name":null}]
          )] | length' "$evidence/processed-after.json")"
          inbox_count="$(jq '.data.items | length' "$evidence/inbox-after.json")"
          if [ "$processed_count" -eq 1 ] && [ "$inbox_count" -eq 0 ]; then break; fi
          sleep 2
        done
        jq -e \
          --arg subject "$probe_subject" \
          '[.data.items[] | select(
            .subject == $subject and
            .from == [{"email":"demo-user@outfitter.test","name":"M1 demo sender"}] and
            .to == [{"email":"researcher@outfitter.test","name":null}]
          )] | length == 1' "$evidence/processed-after.json" >/dev/null
        jq -e '.data.items | length == 0' "$evidence/inbox-after.json" >/dev/null

        # 4) pi auth was seeded into the workspace.
        kubectl -n agent-researcher exec deployment/agent-runtime -- \
          test -s /workspace/.pi/agent/auth.json

        # 5) Statelessness: restart the pod; the message stays processed and is
        #    NOT replied to a second time (dedup is server-side mailbox state).
        kubectl -n agent-researcher rollout restart deployment/agent-runtime
        kubectl -n agent-researcher rollout status deployment/agent-runtime --timeout=3m
        sleep 10
        researcher_xin messages search "in:inbox subject:$probe_token" --max 10 \
          > "$evidence/inbox-after-restart.json"
        jq -e '.data.items | length == 0' "$evidence/inbox-after-restart.json" >/dev/null
        demo_user_xin messages search "in:inbox subject:$probe_token" --max 10 \
          > "$evidence/replies-after-restart.json"
        jq -e '[.data.items[] | select(
          .from == [{"email":"researcher@outfitter.test","name":"M1 researcher agent"}] and
          .to == [{"email":"demo-user@outfitter.test","name":null}]
        )] | length == 1' "$evidence/replies-after-restart.json" >/dev/null

        kubectl get agent researcher -o yaml > "$evidence/agent.yaml"
        echo "M1 email reply verified with exact threading headers; original moved to Processed and was not re-replied after restart: $probe_subject"
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
        if [ "''${CONFIRM_AGENT_CLUSTER_RESET:-}" != "destroy-agent-cluster" ]; then
          echo "Refusing to delete ${clusterState}."
          echo "Re-run with CONFIRM_AGENT_CLUSTER_RESET=destroy-agent-cluster."
          exit 1
        fi
        devenv processes stop cluster || true
        rm -f ${clusterState}/k3s.img ${clusterState}/control.socket
        rm -rf ${clusterShare}
        echo "deleted the local cluster disk, shared image cache, and kubeconfig"
      '';
    };
  };

  enterShell = lib.mkIf (!config.container.isBuilding) ''
    echo "agent-operator devenv: go $(go version | cut -d' ' -f3), kubebuilder $(kubebuilder version 2>/dev/null | head -n1)"
    echo "  cluster: devenv tasks run cluster:up"
    echo "  checks:  devenv tasks run operator:check"
  '';
}
