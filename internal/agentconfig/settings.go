package agentconfig

import "fmt"

const commentKey = "_comment"

func mergeSettings(current, patch []byte) (merged string, changed bool, err error) {
	base, err := decodeObject(current)
	if err != nil {
		return "", false, fmt.Errorf("settings.json: %w", err)
	}
	original := renderObject(base)
	wanted, err := decodeObject(patch)
	if err != nil {
		return "", false, fmt.Errorf("agents/claude/settings.json: %w", err)
	}
	mergeInto(base, wanted)
	merged = renderObject(base)
	return merged, merged != original, nil
}

func mergeInto(base, patch *object) {
	for _, key := range patch.keys {
		if key == commentKey {
			continue
		}
		value := patch.values[key]
		if patchObject, ok := value.(*object); ok {
			if baseObject, ok := base.values[key].(*object); ok {
				mergeInto(baseObject, patchObject)
				continue
			}
		}
		base.set(key, value)
	}
}
