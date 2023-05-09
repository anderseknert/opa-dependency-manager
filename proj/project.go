package proj

import (
	"crypto/sha256"
	"fmt"
	"github.com/go-git/go-git/v5"
	"gopkg.in/yaml.v3"
	"os"
	"strings"
	"styra.com/styrainc/odm/utils"
)

type Project struct {
	Name         string       `yaml:"name,omitempty"`
	Version      string       `yaml:"version,omitempty"`
	SourceDir    string       `yaml:"source,omitempty"`
	Dependencies Dependencies `yaml:"dependencies,omitempty"`
}

type DependencyInfo struct {
	Namespace string `yaml:"namespace,omitempty"`
	Version   string `yaml:"version,omitempty"`
}

type Dependency struct {
	DependencyInfo         `yaml:",inline"`
	Location               string       `yaml:"-"`
	TransitiveDependencies Dependencies `yaml:"-"`
}

type Dependencies map[string]Dependency

func NewProject() *Project {
	return &Project{
		Dependencies: make(map[string]Dependency),
	}
}

func (ds *Dependencies) UnmarshalYAML(unmarshal func(interface{}) error) error {
	infos := make(map[string]DependencyInfo)
	if err := unmarshal(&infos); err != nil {
		return err
	}

	*ds = make(map[string]Dependency)
	for k, v := range infos {
		(*ds)[k] = Dependency{
			DependencyInfo: v,
			Location:       k,
		}
	}

	return nil
}

func (ds *Dependencies) MarshalYAML() (interface{}, error) {
	depMap := make(map[string]Dependency)
	for _, dep := range *ds {
		depMap[dep.Location] = dep
	}

	return depMap, nil
}

func (d *Dependency) Update(rootDir string) error {
	var id string
	if d.Namespace != "" {
		id = d.Namespace
	} else {
		h := sha256.New()
		h.Write([]byte(d.Location))
		id = fmt.Sprintf("%x", h.Sum(nil))
	}

	targetDir := fmt.Sprintf("%s/%s", rootDir, id)

	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory %s: %w", targetDir, err)
	}

	if strings.HasPrefix(d.Location, "git+") {
		if err := d.updateGit(targetDir); err != nil {
			return err
		}
	} else {
		if err := d.updateLocal(targetDir); err != nil {
			return err
		}
	}

	return d.updateTransitive(targetDir)
}

func (d *Dependency) updateLocal(targetDir string) error {
	sourceLocation, err := utils.NormalizeFilePath(d.Location)
	if err != nil {
		return err
	}

	if !utils.FileExists(sourceLocation) {
		return fmt.Errorf("dependency %s does not exist", sourceLocation)
	}

	if !utils.IsDir(sourceLocation) && utils.GetFileName(sourceLocation) == "opa.project" {
		sourceLocation = utils.GetParentDir(sourceLocation)
	}

	// Ignore empty files, as an empty module will break the 'opa refactor' command
	if err := utils.CopyAll(sourceLocation, targetDir, []string{".opa"}, true); err != nil {
		return err
	}

	return nil
}

// TODO: Handle tags, branches and commit hashes
func (d *Dependency) updateGit(targetDir string) error {
	gitUrl := strings.TrimPrefix(d.Location, "git+")
	_, err := git.PlainClone(targetDir, false, &git.CloneOptions{
		URL:      gitUrl,
		Progress: os.Stdout,
	})
	if err != nil {
		return fmt.Errorf("failed to clone git repository %s: %w", gitUrl, err)
	}

	return nil
}

func (d *Dependency) updateTransitive(targetDir string) error {
	transitiveProjectFile := fmt.Sprintf("%s/opa.project", targetDir)
	if utils.FileExists(transitiveProjectFile) {
		transitiveProject, err := ReadProjectFromFile(transitiveProjectFile, false)
		if err != nil {
			return err
		}

		transitiveRootDir := fmt.Sprintf("%s/_transitive", targetDir)
		for _, dep := range transitiveProject.Dependencies {
			if err := dep.Update(transitiveRootDir); err != nil {
				return err
			}
		}

		d.TransitiveDependencies = transitiveProject.Dependencies
	}

	if d.Namespace != "" {
		mapping := fmt.Sprintf("data:data.%s", d.Namespace)
		if _, err := utils.RunCommand("opa", "refactor", "move", "-w", "-p", mapping, targetDir); err != nil {
			return fmt.Errorf("failed to refactor namespace %s: %w", d.Namespace, err)
		}
	}

	return nil
}

func (p *Project) SetDependency(location string, info DependencyInfo) {
	if p.Dependencies == nil {
		p.Dependencies = make(map[string]Dependency)
	}
	p.Dependencies[location] = Dependency{
		DependencyInfo: info,
		Location:       location,
	}
}

func ReadProjectFromFile(path string, allowMissing bool) (*Project, error) {
	path = normalizeProjectPath(path)

	if !utils.FileExists(path) {
		if allowMissing {
			return NewProject(), nil
		} else {
			return nil, fmt.Errorf("project file %s does not exist", path)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read project file %s: %w", path, err)
	}

	var project Project
	err = yaml.Unmarshal(data, &project)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal project file %s: %w", path, err)
	}

	return &project, nil
}

func (p *Project) WriteToFile(path string, override bool) error {
	path = normalizeProjectPath(path)

	if !override && utils.FileExists(path) {
		return fmt.Errorf("project file %s already exists", path)
	}

	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal project file %s: %w", path, err)
	}

	err = os.WriteFile(path, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write project file %s: %w", path, err)
	}

	return nil
}

func normalizeProjectPath(path string) string {
	l := len(path)
	if l >= 11 && path[l-11:] == "opa.project" {
		return path
	} else if l >= 1 && path[l-1] == '/' {
		return path + "opa.project"
	} else {
		return path + "/opa.project"
	}
}
