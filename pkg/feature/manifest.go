package feature

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
)

type Manifest interface {
	// Process allows any arbitrary struct to be passed and used while processing the content of the manifest.
	Process(data any) ([]*unstructured.Unstructured, error)
	// MarkAsManaged sets all non-patch objects to be managed/reconciled by setting the annotation.
	MarkAsManaged(objects []*unstructured.Unstructured)
}

type rawManifest struct {
	name,
	path string
	patch bool
	fsys  fs.FS
}

var _ Manifest = (*rawManifest)(nil)

func (b *rawManifest) Process(_ any) ([]*unstructured.Unstructured, error) {
	manifestFile, openErr := b.fsys.Open(b.path)
	if openErr != nil {
		return nil, fmt.Errorf("failed opening file %s: %w", b.path, openErr)
	}
	defer manifestFile.Close()

	content, readErr := io.ReadAll(manifestFile)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", b.path, readErr)
	}
	resources := string(content)

	unstructuredObjs, convertErr := conversion.StrToUnstructured(resources)
	if convertErr != nil {
		return nil, fmt.Errorf("failed to convert resources defined in %s to unstructured objects: %w", b.path, convertErr)
	}

	return unstructuredObjs, nil
}

func (b *rawManifest) MarkAsManaged(objects []*unstructured.Unstructured) {
	if !b.patch {
		markAsManaged(objects)
	}
}

var _ Manifest = (*templateManifest)(nil)

type templateManifest struct {
	name,
	path string
	patch bool
	fsys  fs.FS
}

func (t *templateManifest) Process(data any) ([]*unstructured.Unstructured, error) {
	manifestFile, openErr := t.fsys.Open(t.path)
	if openErr != nil {
		return nil, fmt.Errorf("failed opening file %s: %w", t.path, openErr)
	}
	defer manifestFile.Close()

	content, readErr := io.ReadAll(manifestFile)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", t.path, readErr)
	}

	tmpl, parseErr := template.New(t.name).
		Option("missingkey=error").
		Parse(string(content))
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", t.path, parseErr)
	}

	var buffer bytes.Buffer
	if executeTmplErr := tmpl.Execute(&buffer, data); executeTmplErr != nil {
		return nil, fmt.Errorf("failed to execute template %s: %w", t.path, executeTmplErr)
	}

	resources := buffer.String()

	unstructuredObjs, convertErr := conversion.StrToUnstructured(resources)
	if convertErr != nil {
		return nil, fmt.Errorf("failed to convert resources defined in %s to unstructured objects: %w", t.path, convertErr)
	}

	return unstructuredObjs, nil
}

func (t *templateManifest) MarkAsManaged(objects []*unstructured.Unstructured) {
	if !t.patch {
		markAsManaged(objects)
	}
}

func loadManifestsFrom(fsys fs.FS, path string) ([]Manifest, error) {
	var manifests []Manifest

	err := fs.WalkDir(fsys, path, func(path string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		_, err := dirEntry.Info()
		if err != nil {
			return err
		}

		if dirEntry.IsDir() {
			return nil
		}
		if isTemplateManifest(path) {
			manifests = append(manifests, CreateTemplateManifestFrom(fsys, path))
		} else {
			manifests = append(manifests, CreateRawManifestFrom(fsys, path))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return manifests, nil
}

func CreateRawManifestFrom(fsys fs.FS, path string) *rawManifest {
	basePath := filepath.Base(path)

	return &rawManifest{
		name:  basePath,
		path:  path,
		patch: strings.Contains(basePath, ".patch."),
		fsys:  fsys,
	}
}

func CreateTemplateManifestFrom(fsys fs.FS, path string) *templateManifest {
	basePath := filepath.Base(path)

	return &templateManifest{
		name:  basePath,
		path:  path,
		patch: strings.Contains(basePath, ".patch."),
		fsys:  fsys,
	}
}

func isTemplateManifest(path string) bool {
	return strings.Contains(filepath.Base(path), ".tmpl.")
}
