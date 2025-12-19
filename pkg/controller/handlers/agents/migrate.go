package agents

import (
	"fmt"

	"github.com/obot-platform/nah/pkg/name"
	"github.com/obot-platform/nah/pkg/router"
	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MigrateAllowedModels(req router.Request, _ router.Response) error {
	agent := req.Object.(*v1.Agent)

	// Skip if AllowedModels is already empty (either already migrated or never had restrictions)
	if len(agent.Spec.Manifest.AllowedModels) == 0 {
		return nil
	}

	// Try to get existing Model Permission Rule
	var (
		rule     v1.ModelPermissionRule
		ruleName = name.SafeConcatName(system.ModelPermissionRulePrefix, agent.Name)
	)
	if err := req.Get(&rule, agent.Namespace, ruleName); err == nil {
		// Rule already exists - check if models match
		if modelsMatch(rule.Spec.Manifest.Models, agent.Spec.Manifest.AllowedModels) {
			// Models match, just clear AllowedModels and return
			agent.Spec.Manifest.AllowedModels = nil
			return req.Client.Update(req.Ctx, agent)
		}

		// Models don't match, update the rule
		rule.Spec.Manifest.Models = convertToModelResources(agent.Spec.Manifest.AllowedModels)
		if err := req.Client.Update(req.Ctx, &rule); err != nil {
			return fmt.Errorf("failed to update model permission rule for agent %s: %w", agent.Name, err)
		}
	} else if apierrors.IsNotFound(err) {
		// Create new Model Permission Rule
		rule = v1.ModelPermissionRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ruleName,
				Namespace: agent.Namespace,
			},
			Spec: v1.ModelPermissionRuleSpec{
				Manifest: types.ModelPermissionRuleManifest{
					DisplayName: fmt.Sprintf("%s Allowed Models", agent.Spec.Manifest.Name),
					Subjects: []types.Subject{
						{
							Type: types.SubjectTypeSelector,
							ID:   "*",
						},
					},
					Models: convertToModelResources(agent.Spec.Manifest.AllowedModels),
				},
			},
		}

		if err := req.Client.Create(req.Ctx, &rule); err != nil {
			return fmt.Errorf("failed to create model permission rule for agent %s: %w", agent.Name, err)
		}
	} else {
		return err
	}

	// Clear AllowedModels from the agent
	agent.Spec.Manifest.AllowedModels = nil
	return req.Client.Update(req.Ctx, agent)
}

// convertToModelResources converts a list of model IDs to ModelResources
func convertToModelResources(modelIDs []string) []types.ModelResource {
	models := make([]types.ModelResource, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		models = append(models, types.ModelResource{
			ModelID: modelID,
		})
	}
	return models
}

// modelsMatch checks if the existing ModelResources match the AllowedModels list
func modelsMatch(existingModels []types.ModelResource, allowedModels []string) bool {
	if len(existingModels) != len(allowedModels) {
		return false
	}

	// Create a map of existing model IDs for quick lookup
	existingSet := make(map[string]struct{}, len(existingModels))
	for _, model := range existingModels {
		existingSet[model.ModelID] = struct{}{}
	}

	// Check if all allowed models are in the existing set
	for _, modelID := range allowedModels {
		if _, exists := existingSet[modelID]; !exists {
			return false
		}
	}

	return true
}
