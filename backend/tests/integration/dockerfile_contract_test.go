package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerfileNonRootContract(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}

	dockerfile := string(contents)

	for _, token := range []string{
		"groupadd --gid 1000 prism",
		"useradd --uid 1000 --gid 1000",
		"mkdir -p /app/config",
		"RUN chown -R prism:prism /app/config /app/backend",
		"ENV PRISM_CONFIG_PATH=/app/config/config.json",
		"USER prism:prism",
		"CMD [\"prism-backend\"]",
	} {
		if !strings.Contains(dockerfile, token) {
			t.Fatalf("Dockerfile missing required token %q", token)
		}
	}

	userIndex := strings.Index(dockerfile, "USER prism:prism")
	cmdIndex := strings.Index(dockerfile, "CMD [\"prism-backend\"]")
	if userIndex > cmdIndex {
		t.Fatalf("USER prism:prism must appear before CMD [\"prism-backend\"]")
	}

	groupIndex := strings.Index(dockerfile, "groupadd --gid 1000 prism")
	useraddIndex := strings.Index(dockerfile, "useradd --uid 1000 --gid 1000")
	if groupIndex > useraddIndex {
		t.Fatalf("groupadd --gid 1000 prism must appear before useradd --uid 1000 --gid 1000")
	}
}
