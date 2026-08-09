package integrationtest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerfileSingleImageContract(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "Dockerfile"))
	if err != nil {
		t.Fatalf("read root Dockerfile: %v", err)
	}

	dockerfile := string(contents)
	for _, token := range []string{
		"FROM golang:",
		"FROM node:",
		"COPY --from=backend-builder /out/prism-backend /usr/local/bin/prism-backend",
		"COPY backend/migrations ./migrations",
		"COPY --from=frontend-builder /out/html/ /usr/share/nginx/html/",
		"COPY docker/nginx.conf.template /etc/prism/nginx.conf.template",
		"COPY docker/entrypoint.sh /usr/local/bin/prism-entrypoint",
		"ENV PRISM_CONFIG_PATH=/app/config/config.json",
		"EXPOSE 8080",
		"USER prism:prism",
		"ENTRYPOINT [\"prism-entrypoint\"]",
	} {
		if !strings.Contains(dockerfile, token) {
			t.Fatalf("root Dockerfile missing required single-image token %q", token)
		}
	}
}
