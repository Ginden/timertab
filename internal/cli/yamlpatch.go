package cli

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/ginden/timertab/internal/config"
)

// patchConfigYAML parses raw as a YAML config document, applies mutate to the root
// mapping node, and re-encodes the tree so comments and key order survive.
func patchConfigYAML(raw []byte, mutate func(doc *yaml.Node) error) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return nil, fmt.Errorf("invalid yaml document structure")
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("yaml root must be a mapping")
	}

	if err := mutate(doc); err != nil {
		return nil, err
	}

	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(&root); err != nil {
		_ = encoder.Close()
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

func jobsSequenceNode(doc *yaml.Node) (*yaml.Node, error) {
	jobsNode := mappingNodeValue(doc, "jobs")
	if jobsNode == nil {
		return nil, fmt.Errorf("jobs key not found")
	}
	if jobsNode.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("jobs must be a sequence")
	}
	return jobsNode, nil
}

// patchMissingJobIDs appends generated ids to job mappings that lack one, mirroring
// the id injection done by edit so every save persists normalized ids.
func patchMissingJobIDs(jobsNode *yaml.Node, jobs []config.Job) error {
	if len(jobsNode.Content) != len(jobs) {
		return fmt.Errorf("jobs length mismatch")
	}
	for idx, jobNode := range jobsNode.Content {
		if jobNode.Kind != yaml.MappingNode {
			return fmt.Errorf("jobs[%d] must be a mapping", idx)
		}
		if mappingNodeValue(jobNode, "id") != nil {
			continue
		}
		jobNode.Content = append(jobNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "id"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: jobs[idx].ID},
		)
	}
	return nil
}

// setJobEnabledNode patches (or inserts) the enabled scalar in a job mapping while
// keeping any comments attached to the existing value node.
func setJobEnabledNode(jobNode *yaml.Node, enabled bool) error {
	if jobNode == nil || jobNode.Kind != yaml.MappingNode {
		return fmt.Errorf("job entry must be a mapping")
	}

	value := "false"
	if enabled {
		value = "true"
	}

	if existing := mappingNodeValue(jobNode, "enabled"); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!bool"
		existing.Value = value
		existing.Style = 0
		existing.Content = nil
		existing.Anchor = ""
		existing.Alias = nil
		return nil
	}

	jobNode.Content = append(jobNode.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: value},
	)
	return nil
}

func removeJobNode(jobsNode *yaml.Node, index int) error {
	if index < 0 || index >= len(jobsNode.Content) {
		return fmt.Errorf("job index %d out of range", index)
	}
	jobsNode.Content = append(jobsNode.Content[:index], jobsNode.Content[index+1:]...)
	return nil
}

func appendJobNodes(jobsNode *yaml.Node, jobs []config.Job) error {
	for _, job := range jobs {
		node := &yaml.Node{}
		if err := node.Encode(job); err != nil {
			return fmt.Errorf("encode job %q: %w", job.ID, err)
		}
		jobsNode.Content = append(jobsNode.Content, node)
	}
	// A previously empty flow sequence ([]) must switch to block style so the
	// appended mappings render as regular multi-line entries.
	if len(jobsNode.Content) > 0 {
		jobsNode.Style = 0
	}
	return nil
}

// savePatchedConfig writes loaded to path, preferring a node-level patch of the
// original file bytes so user comments and formatting survive. patch receives the
// jobs sequence node with generated ids for preJobs already injected. It falls back
// to canonical marshaling when node patching is not possible.
func savePatchedConfig(path string, raw []byte, loaded *config.File, preJobs []config.Job, patch func(jobsNode *yaml.Node) error) error {
	if len(raw) > 0 {
		out, err := patchConfigYAML(raw, func(doc *yaml.Node) error {
			jobsNode, err := jobsSequenceNode(doc)
			if err != nil {
				return err
			}
			if err := patchMissingJobIDs(jobsNode, preJobs); err != nil {
				return err
			}
			return patch(jobsNode)
		})
		if err == nil {
			return writeConfigFile(path, out)
		}
	}
	return saveConfig(path, loaded)
}
