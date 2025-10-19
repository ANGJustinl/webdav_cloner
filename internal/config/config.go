package config

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Jobs []Job `yaml:"jobs"`
}

type Job struct {
	Name        string     `yaml:"name"`
	Source      Endpoint   `yaml:"source"`
	Targets     []Endpoint `yaml:"targets"`
	Path        string     `yaml:"path"`
	Concurrency int        `yaml:"concurrency"`
}

type Endpoint struct {
	URL         string `yaml:"url"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	PasswordEnv string `yaml:"password_env"`
	Root        string `yaml:"root"`
}

func Load(p string) (*Config, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("open config %q: %w", p, err)
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", p, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode config %q: %w", p, err)
	}

	if err := cfg.normalise(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) normalise() error {
	if len(c.Jobs) == 0 {
		return fmt.Errorf("no jobs defined in configuration")
	}

	for i := range c.Jobs {
		job := &c.Jobs[i]
		job.Name = strings.TrimSpace(job.Name)
		if job.Name == "" {
			job.Name = fmt.Sprintf("job-%d", i+1)
		}

		job.Path = strings.Trim(job.Path, "/")

		if err := job.Source.prepare(job.Name, "source"); err != nil {
			return err
		}

		if len(job.Targets) == 0 {
			return fmt.Errorf("%s: no targets defined", job.Name)
		}

		for j := range job.Targets {
			if err := job.Targets[j].prepare(job.Name, fmt.Sprintf("target-%d", j+1)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (e *Endpoint) prepare(jobName, role string) error {
	e.URL = strings.TrimSpace(e.URL)
	if e.URL == "" {
		return fmt.Errorf("%s (%s): missing url", jobName, role)
	}

	if e.Password == "" && e.PasswordEnv != "" {
		value := os.Getenv(e.PasswordEnv)
		if value == "" {
			return fmt.Errorf("%s (%s): environment variable %q is empty", jobName, role, e.PasswordEnv)
		}
		e.Password = value
	}

	e.Root = normaliseRoot(e.Root)

	return nil
}

func normaliseRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" || root == "." || root == "/" {
		return "/"
	}

	root = strings.TrimSuffix(root, "/")
	if !strings.HasPrefix(root, "/") {
		root = "/" + root
	}

	clean := path.Clean(root)
	if clean == "." {
		return "/"
	}
	return clean
}
