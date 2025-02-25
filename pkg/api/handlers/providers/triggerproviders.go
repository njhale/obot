package providers

import (
	"encoding/json"
	"fmt"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
)

func ConvertTriggerProviderToolRef(toolRef v1.ToolReference, cred map[string]string) (*types.TriggerProviderStatus, error) {
	var (
		providerMeta   ProviderMeta
		missingEnvVars []string
		tool           = toolRef.Status.Tool
	)
	if tool != nil {
		if rawProviderMeta := tool.Metadata["providerMeta"]; rawProviderMeta != "" {
			if err := json.Unmarshal([]byte(rawProviderMeta), &providerMeta); err != nil {
				return nil, fmt.Errorf("failed to unmarshal provider metadata for %s: %v", rawProviderMeta, err)
			}
		}

		for _, envVar := range providerMeta.EnvVars {
			if _, ok := cred[envVar.Name]; !ok {
				missingEnvVars = append(missingEnvVars, envVar.Name)
			}
		}
	}

	return &types.TriggerProviderStatus{
		CommonProviderMetadata:          providerMeta.CommonProviderMetadata,
		ObotScopes:                      providerMeta.ObotScopes,
		Configured:                      tool != nil && len(missingEnvVars) == 0,
		RequiredConfigurationParameters: providerMeta.EnvVars,
		OptionalConfigurationParameters: providerMeta.OptionalEnvVars,
		MissingConfigurationParameters:  missingEnvVars,
	}, nil
}
