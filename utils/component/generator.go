package component

import (
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue"
	"github.com/layer5io/meshkit/models/meshmodel/core/v1alpha1"
	"github.com/layer5io/meshkit/utils"
	"github.com/layer5io/meshkit/utils/manifests"
)

const ComponentMetaNameKey = "name"

// all paths should be a valid CUE expression
type CuePathConfig struct {
	NamePath       string
	GroupPath      string
	VersionPath    string
	SpecPath       string
	ScopePath      string
	PropertiesPath string
	// identifiers are the values that uniquely identify a CRD (in most of the cases, it is the 'Name' field)
	IdentifierPath string
}

var DefaultPathConfig = CuePathConfig{
	NamePath:       "spec.names.kind",
	IdentifierPath: "spec.names.kind",
	VersionPath:    "spec.versions[0].name",
	GroupPath:      "spec.group",
	ScopePath:      "spec.scope",
	SpecPath:       "spec.versions[0].schema.openAPIV3Schema.properties.spec",
	PropertiesPath: "properties",
}

var DefaultPathConfig2 = CuePathConfig{
	NamePath:       "spec.names.kind",
	IdentifierPath: "spec.names.kind",
	VersionPath:    "spec.versions[0].name",
	GroupPath:      "spec.group",
	ScopePath:      "spec.scope",
	SpecPath:       "spec.validation.openAPIV3Schema.properties.spec",
}

var Configs = []CuePathConfig{DefaultPathConfig, DefaultPathConfig2}

func Generate(crd string) (v1alpha1.ComponentDefinition, error) {
	component := v1alpha1.ComponentDefinition{}
	component.Metadata = make(map[string]interface{})
	crdCue, err := utils.YamlToCue(crd)
	if err != nil {
		return component, err
	}
	var schema string
	for _, cfg := range Configs {
		schema, err = getSchema(crdCue, cfg)
		if err == nil {
			break
		}
	}
	component.Schema = schema
	name, err := extractCueValueFromPath(crdCue, DefaultPathConfig.NamePath)
	if err != nil {
		return component, err
	}
	version, err := extractCueValueFromPath(crdCue, DefaultPathConfig.VersionPath)
	if err != nil {
		return component, err
	}
	group, err := extractCueValueFromPath(crdCue, DefaultPathConfig.GroupPath)
	if err != nil {
		return component, err
	}
	// return component, err Ignore error if scope isn't found
	scope, _ := extractCueValueFromPath(crdCue, DefaultPathConfig.ScopePath)
	if scope == "Cluster" {
		component.Metadata["isNamespaced"] = false
	} else if scope == "Namespaced" {
		component.Metadata["isNamespaced"] = true
	}
	component.Kind = name
	if group != "" {
		component.APIVersion = fmt.Sprintf("%s/%s", group, version)
	} else {
		component.APIVersion = version
	}

	component.Format = v1alpha1.JSON
	component.DisplayName = manifests.FormatToReadableString(name)
	return component, nil
}

/*
  We walk the entire schema, looking for specfic peroperties that requires modification and store their path.
  After the walk is complete, we iterate all paths and do the modification.
  If any error occurs while updating schema properties, we return nil and skip the update.
*/
func UpdateProperties(fieldVal cue.Value, cuePath cue.Path, group string) map[string]interface{} {
	rootPath := fieldVal.Path().Selectors()

	compProperties := fieldVal.LookupPath(cuePath)
	crd, err := fieldVal.MarshalJSON()
	if err != nil {
		return nil
	}

	modified := make(map[string]interface{})
	pathSelectors := [][]cue.Selector{}

	err = json.Unmarshal(crd, &modified)
	if err != nil {
		return nil
	}

	compProperties.Walk(func(c cue.Value) bool {
		return true
	}, func(c cue.Value) {
		val := c.LookupPath(cue.ParsePath(`"x-kubernetes-preserve-unknown-fields"`))
		if val.Exists() {
			child := val.Path().Selectors()
			childM := child[len(rootPath):(len(child) - 1)]
			pathSelectors = append(pathSelectors, childM)
		}
	})

	/* 
	  "pathSelectors" contains all the paths from root to the property which needs to be modified.
	*/
	for _, selectors := range pathSelectors {
		var m interface{}
		m = modified
		index := 0

		for index < len(selectors) {
			selector := selectors[index]
			selectorType := selector.Type()
			s := selector.String()

			if selectorType == cue.IndexLabel {
				t := m.([]interface{})
				token := selector.Index()
				m = t[token].(map[string]interface{})
			} else {
				if selectorType == cue.StringLabel {
					s = selector.Unquoted()
				}
				t := m.(map[string]interface{})
				m = t[s]
			}
			index++
		}

		t := m.(map[string]interface{})
		delete(t, "x-kubernetes-preserve-unknown-fields")
		if m == nil {
			m = make(map[string]interface{})
		}
		t["type"] = "string"
		t["format"] = "textarea"
	}
	return modified
}
