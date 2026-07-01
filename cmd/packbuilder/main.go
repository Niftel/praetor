// Command packbuilder builds Execution Packs from their YAML spec. It polls the
// execution_packs table for pending packs, runs the parameterised Dockerfile via
// the Docker daemon, extracts the pack tarball into build/runtime/ (shared with
// the executor), and marks the pack ready or failed. This is what makes "define
// a pack from YAML in Praetor" actually produce the pack.
package main

import (
	"database/sql"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/praetordev/praetor/pkg/db"
	"gopkg.in/yaml.v3"
)

// Spec is the Execution Pack definition (mirrors cmd/execpack).
type Spec struct {
	Python      string   `yaml:"python"`
	Ansible     string   `yaml:"ansible"`
	Pip         []string `yaml:"pip"`
	Collections []string `yaml:"collections"`
	Arches      []string `yaml:"arches"`
}

func main() {
	log.Println("Execution Pack builder starting...")
	database, err := db.InitDB()
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	for {
		var pack struct {
			ID   int64          `db:"id"`
			Name string         `db:"name"`
			Spec sql.NullString `db:"spec"`
		}
		if err := database.Get(&pack, `SELECT id, name, spec FROM execution_packs WHERE status='pending' ORDER BY id LIMIT 1`); err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		log.Printf("Building Execution Pack %q (id %d)...", pack.Name, pack.ID)
		database.Exec(`UPDATE execution_packs SET status='building', build_log=NULL WHERE id=$1`, pack.ID)

		out, berr := buildPack(pack.Name, pack.Spec.String)
		status := "ready"
		if berr != nil {
			status = "failed"
			out += "\n\nBUILD FAILED: " + berr.Error()
			log.Printf("Pack %q build failed: %v", pack.Name, berr)
		} else {
			log.Printf("Pack %q built.", pack.Name)
		}
		database.Exec(`UPDATE execution_packs SET status=$1, build_log=$2 WHERE id=$3`, status, tail(out, 8000), pack.ID)
	}
}

// buildPack builds every arch of a pack from its YAML spec and extracts the
// tarball(s) into /build/runtime (shared with the executor).
func buildPack(name, specYAML string) (string, error) {
	var spec Spec
	if strings.TrimSpace(specYAML) != "" {
		if err := yaml.Unmarshal([]byte(specYAML), &spec); err != nil {
			return "", fmt.Errorf("parse spec: %w", err)
		}
	}
	if spec.Python == "" {
		spec.Python = "3.11.9"
	}
	if spec.Ansible == "" {
		spec.Ansible = "ansible"
	}
	if len(spec.Arches) == 0 {
		spec.Arches = []string{"arm64"}
	}

	var out strings.Builder
	for _, arch := range spec.Arches {
		img := "praetor-execpack-" + name + "-" + arch
		build := exec.Command("docker", "build", "--target", "build",
			"--build-arg", "TARGETARCH="+arch,
			"--build-arg", "PY_VERSION="+spec.Python,
			"--build-arg", "ANSIBLE_SPEC="+spec.Ansible,
			"--build-arg", "EXTRA_PIP="+strings.Join(spec.Pip, " "),
			"--build-arg", "GALAXY_COLLECTIONS="+strings.Join(spec.Collections, " "),
			"--build-arg", "PACK_NAME="+name,
			"-t", img, "/build/ansible-runtime")
		b, err := build.CombinedOutput()
		out.Write(b)
		if err != nil {
			return out.String(), fmt.Errorf("docker build (%s): %w", arch, err)
		}

		// Extract the pack tarball from the built image into the shared dir.
		cid, err := exec.Command("docker", "create", img).Output()
		if err != nil {
			return out.String(), fmt.Errorf("docker create: %w", err)
		}
		id := strings.TrimSpace(string(cid))
		if cp, err := exec.Command("docker", "cp", id+":/out/.", "/build/runtime/").CombinedOutput(); err != nil {
			out.Write(cp)
			return out.String(), fmt.Errorf("docker cp: %w", err)
		}
		exec.Command("docker", "rm", id).Run()
		exec.Command("docker", "rmi", img).Run()
		out.WriteString(fmt.Sprintf("\nbuilt %s-linux-%s.tar.gz\n", name, arch))
	}
	return out.String(), nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "...(truncated)...\n" + s[len(s)-n:]
}
