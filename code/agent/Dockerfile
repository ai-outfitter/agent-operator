FROM docker.io/library/golang:1.26@sha256:3aff6657219a4d9c14e27fb1d8976c49c29fddb70ba835014f477e1c70636647 AS agent-builder

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/link-agent ./cmd/link-agent

FROM docker.io/library/node:22.19.0-bookworm@sha256:afff6d8c97964a438d2e6a9c96509367e45d8bf93f790ad561a1eaea926303d9 AS outfitter-builder
ARG OUTFITTER_REVISION=c44205ef35265c893ad9f088772c35c71753bfb7

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates git \
  && rm -rf /var/lib/apt/lists/*
RUN git clone https://github.com/ai-outfitter/outfitter.git /src/outfitter \
  && git -C /src/outfitter checkout --detach "${OUTFITTER_REVISION}"
WORKDIR /src/outfitter
RUN npm pkg delete scripts.prepare \
  && npm pkg delete scripts.prepare --workspace @ai-outfitter/outfitter \
  && npm ci \
  && npm run build \
  && npm prune --omit=dev

FROM docker.io/library/node:22.19.0-bookworm-slim@sha256:4a4884e8a44826194dff92ba316264f392056cbe243dcc9fd3551e71cea02b90

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates git openssh-client tar \
  && rm -rf /var/lib/apt/lists/*

COPY --from=outfitter-builder /src/outfitter /opt/outfitter
COPY --from=agent-builder /out/link-agent /usr/local/bin/link-agent
RUN ln -sf /opt/outfitter/code/cli/dist/cli.js /usr/local/bin/outfitter \
  && ln -sf /opt/outfitter/node_modules/.bin/pi /usr/local/bin/pi \
  && mkdir -p /workspace/.link /workspace/.pi/agent \
  && chown -R node:node /workspace

ENV HOME=/workspace
USER 1000:1000
WORKDIR /workspace

ENTRYPOINT ["link-agent"]
CMD ["run"]
