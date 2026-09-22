package residentprovisioner

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	api "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var workspacePattern = regexp.MustCompile(`^(user|org):[1-9][0-9]{0,19}$`)
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var imagePattern = regexp.MustCompile(`^[A-Za-z0-9./:_-]+@sha256:[0-9a-f]{64}$`)

// Config is operator-owned. No catalog, image, model or quota input comes from the caller.
type Config struct {
	Token             string
	CatalogRepository string
	CatalogRevision   string
	RuntimeImage      string
	Model             string
	ServiceOrigins    []string
	OperatorNamespace string
	RuntimePath       string
	AllowLocalHTTP    bool
	Workspace         api.WorkspaceSpec
}

func (c *Config) Validate() error {
	if len(c.Token) < 32 || strings.ContainsAny(c.Token, " \r\n\t") {
		return fmt.Errorf("resident provisioner token must contain at least 32 non-whitespace characters")
	}
	if !repositoryPattern.MatchString(c.CatalogRepository) || !revisionPattern.MatchString(c.CatalogRevision) || !imagePattern.MatchString(c.RuntimeImage) {
		return fmt.Errorf("resident catalog and runtime must have immutable commit and digest pins")
	}
	if c.Model == "" || strings.ContainsAny(c.Model, " \r\n\t") || c.OperatorNamespace == "" {
		return fmt.Errorf("resident model and operator namespace are required")
	}
	if c.RuntimePath == "" {
		c.RuntimePath = "/usr/local/bin:/usr/bin:/bin"
	}
	if strings.ContainsAny(c.RuntimePath, "\r\n") {
		return fmt.Errorf("invalid runtime PATH")
	}
	if len(c.ServiceOrigins) == 0 {
		return fmt.Errorf("resident service origin allowlist is required")
	}
	for _, origin := range c.ServiceOrigins {
		if !validOrigin(origin, c.AllowLocalHTTP) {
			return fmt.Errorf("invalid resident service origin")
		}
	}
	if c.Workspace.Volume.Size.IsZero() {
		c.Workspace = defaultWorkspace()
	}
	return nil
}
func validOrigin(raw string, local bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || (local && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}
func defaultWorkspace() api.WorkspaceSpec {
	return api.WorkspaceSpec{
		ResourceQuota: api.ResourceQuotaSpec{Hard: corev1.ResourceList{
			"requests.cpu": resource.MustParse("2"), "requests.memory": resource.MustParse("4Gi"), "limits.cpu": resource.MustParse("4"), "limits.memory": resource.MustParse("8Gi"), "requests.storage": resource.MustParse("40Gi"), "persistentvolumeclaims": resource.MustParse("4"), "count/pods": resource.MustParse("5"), "count/services": resource.MustParse("3"), "count/secrets": resource.MustParse("10"), "count/configmaps": resource.MustParse("10"),
		}},
		LimitRange: api.WorkspaceLimitRangeSpec{Container: api.ContainerLimitSpec{DefaultRequest: corev1.ResourceList{"cpu": resource.MustParse("100m"), "memory": resource.MustParse("128Mi")}, Default: corev1.ResourceList{"cpu": resource.MustParse("1"), "memory": resource.MustParse("1Gi")}}},
		Volume:     api.WorkspaceVolumeSpec{Size: resource.MustParse("10Gi")},
	}
}

type Workspace struct {
	ID    string `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}
type Repository struct {
	ID       int64  `json:"id"`
	FullName string `json:"fullName"`
}
type Request struct {
	Generation          int64        `json:"generation"`
	Workspace           Workspace    `json:"workspace"`
	InstallationID      int64        `json:"installationId"`
	Repositories        []Repository `json:"repositories"`
	ProjectManagerName  string       `json:"projectManagerName"`
	EngineerName        string       `json:"engineerName"`
	ServiceBaseURL      string       `json:"serviceBaseUrl"`
	ProjectManagerToken string       `json:"projectManagerToken"`
	EngineerToken       string       `json:"engineerToken"`
	TaskToken           string       `json:"taskToken"`
}

func (c Config) validateRequest(workspace string, r Request) error {
	if !workspacePattern.MatchString(workspace) || r.Workspace.ID != workspace || r.Generation <= 0 {
		return fmt.Errorf("invalid workspace identity")
	}
	expected := "User"
	if strings.HasPrefix(workspace, "org:") {
		expected = "Organization"
	}
	if r.Workspace.Type != expected || r.Workspace.Login == "" || len(r.Workspace.Login) > 100 || r.InstallationID <= 0 {
		return fmt.Errorf("invalid workspace or installation")
	}
	for _, name := range []string{r.ProjectManagerName, r.EngineerName} {
		if strings.TrimSpace(name) == "" || len(name) > 63 || strings.ContainsAny(name, "\r\n\x00") {
			return fmt.Errorf("resident names must contain 1 to 63 display characters")
		}
	}
	for _, token := range []string{r.ProjectManagerToken, r.EngineerToken, r.TaskToken} {
		if len(token) < 32 || len(token) > 8192 || strings.ContainsAny(token, " \r\n\t") {
			return fmt.Errorf("invalid resident credential")
		}
	}
	if len(r.Repositories) == 0 || len(r.Repositories) > 100 {
		return fmt.Errorf("select 1 to 100 repositories")
	}
	ids := map[int64]bool{}
	names := map[string]bool{}
	for _, repo := range r.Repositories {
		name := strings.ToLower(repo.FullName)
		if repo.ID <= 0 || !repositoryPattern.MatchString(repo.FullName) || ids[repo.ID] || names[name] {
			return fmt.Errorf("invalid or duplicate repository")
		}
		ids[repo.ID] = true
		names[name] = true
	}
	if slices.Contains(c.ServiceOrigins, r.ServiceBaseURL) {
		return nil
	}
	return fmt.Errorf("service origin is not allowed")
}
