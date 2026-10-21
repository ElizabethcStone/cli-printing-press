package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// A multipart/form-data endpoint reached through the code-orchestration
// surface must upload its file params as file parts instead of sending a JSON
// body the endpoint rejects.
func TestCodeOrchestrationSendsMultipartUploads(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("corchupload")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.MCP = spec.MCPConfig{Orchestration: "code"}
	apiSpec.Resources["packages"] = spec.Resource{
		Description: "Packages",
		Endpoints: map[string]spec.Endpoint{
			"import": {
				Method:             "POST",
				Path:               "/packages/import",
				Description:        "Import a package file",
				RequestContentType: "multipart/form-data",
				Body: []spec.Param{
					{Name: "package", Type: "string", Format: "binary", Required: true, Description: "Package file"},
					{Name: "projectId", Type: "string", Description: "Target project"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "corchupload-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	src := readGeneratedFile(t, outputDir, "internal", "mcp", "code_orch.go")
	require.Regexp(t, `Multipart:\s+true,\s+FileParams:\s+\[\]string\{"package"\}`, src,
		"multipart endpoint must mark its binary body param as a file part")

	runtimeTest := `package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestCodeOrchestrationSendsMultipartUploadsRuntime(t *testing.T) {
	var gotType, gotFile, gotField string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart: %v", err)
		}
		gotField = r.FormValue("projectId")
		if f, _, err := r.FormFile("package"); err == nil {
			b, _ := io.ReadAll(f)
			gotFile = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"ok\":true}"))
	}))
	defer server.Close()
	t.Setenv("CORCHUPLOAD_BASE_URL", server.URL)

	pkg := filepath.Join(t.TempDir(), "p.bin")
	if err := os.WriteFile(pkg, []byte("PACKAGE-BYTES"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := handleCodeOrchExecute(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{Arguments: map[string]any{
		"endpoint_id": "packages.import",
		"confirm":     true,
		"params":      map[string]any{"package": pkg, "projectId": "p1"},
	}}})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("upload failed: result=%#v err=%v", result, err)
	}
	if !strings.HasPrefix(gotType, "multipart/form-data") {
		t.Fatalf("content type = %q, want multipart/form-data", gotType)
	}
	if gotFile != "PACKAGE-BYTES" || gotField != "p1" {
		t.Fatalf("server saw file=%q projectId=%q", gotFile, gotField)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "code_orch_multipart_runtime_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "^TestCodeOrchestrationSendsMultipartUploadsRuntime$", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}
