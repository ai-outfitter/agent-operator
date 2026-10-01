# Build manager with CGO_ENABLED=0 go build -o manager ./cmd in code/operator.
FROM gcr.io/distroless/static:nonroot
COPY manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
