package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	"github.com/ai-outfitter/agent-operator/code/operator/internal/controller"
	"github.com/ai-outfitter/agent-operator/code/operator/internal/forgegateway"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(aioutfitterv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	if len(os.Args) > 1 && os.Args[1] == "forge-gateway" {
		runForgeGateway()
		return
	}
	var metricsAddr string
	var agentImage string
	var gatewayImage string
	var outfitterRevision string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	// The published Outfitter container, not an image this repository builds — see #13.
	// An Agent needing more than the stock runtime sets spec.image to one derived from it
	// (`FROM ghcr.io/ai-outfitter/outfitter:<version>`), published from that org's own
	// <org>/.agents repository.
	//
	// 1.4.0 is the first release that can serve as this default at all: earlier images had
	// no shell, so the setup init containers (which run `sh -c`) could not execute, and no
	// root account, so no consumer could derive from it. It is also the first that forwards
	// SIGTERM to the harness, so a resident agent terminates cleanly.
	//
	// A tag rather than a digest is deliberate for a default: it tracks the operator release
	// it shipped with and is legible in `--help`. Deployments should still pin a digest.
	//
	// 1.5.0 is the first Debian-base primary tag; the Nix closure variant publishes behind
	// `-nix`. The controller gates the Nix-store machinery on the image reference (see
	// imageNeedsNixStore), so the default gets no seed init container or store PVC.
	flag.StringVar(&agentImage, "agent-image", "ghcr.io/ai-outfitter/outfitter:1.5.0",
		"Default agent runtime image, used when an Agent does not set spec.image. Deployments should pin a digest.")
	flag.StringVar(&gatewayImage, "gateway-image", "ghcr.io/ai-outfitter/agent-operator:agent-operator-v0.11.1",
		"Agent Operator image used by organization forge gateways.")
	// Reported on Agent.status.outfitterRevision. This is a third hand-maintained pin of one
	// dependency, after flake.lock and devenv.lock, and it had already drifted: it claimed
	// c44205ef (2026-07-18) while the image it described was built from 3d73c233
	// (2026-07-21) — both v0.11.0, so nothing surfaced the disagreement. Now that the runtime
	// is an upstream release with a version in its reference, this should be derived from
	// --agent-image or removed; removal changes a status field, so it needs its own change.
	flag.StringVar(&outfitterRevision, "outfitter-revision", "v1.5.0",
		"Outfitter revision present in the configured agent runtime image.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Client: client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{
			&corev1.Secret{},
			&corev1.ConfigMap{},
		}}},
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "0e0a6b02.aioutfitter.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	if err := (&controller.OrganizationReconciler{
		Client: mgr.GetClient(), Scheme: mgr.GetScheme(), AgentImage: agentImage, GatewayImage: gatewayImage,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "organization")
		os.Exit(1)
	}
	if err := (&controller.AgentReconciler{
		Client:            mgr.GetClient(),
		APIReader:         mgr.GetAPIReader(),
		Scheme:            mgr.GetScheme(),
		AgentImage:        agentImage,
		OutfitterRevision: outfitterRevision,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "agent")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}

func runForgeGateway() {
	var routes []forgegateway.Route
	if err := json.Unmarshal([]byte(os.Getenv("FORGE_ROUTES")), &routes); err != nil {
		slog.Error("invalid FORGE_ROUTES", "error", err)
		os.Exit(2)
	}
	gateway, err := forgegateway.Open(forgegateway.Config{
		Organization: os.Getenv("ORGANIZATION"), Owner: os.Getenv("FORGE_OWNER"),
		Secret: os.Getenv("FORGE_WEBHOOK_SECRET"), SpoolPath: os.Getenv("SPOOL_PATH"), Routes: routes,
	})
	if err != nil {
		slog.Error("open forge gateway", "error", err)
		os.Exit(1)
	}
	defer func() { _ = gateway.Close() }()
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = ":8080"
	}
	server := &http.Server{Addr: address, Handler: gateway.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("serve forge gateway", "error", err)
		os.Exit(1)
	}
}
