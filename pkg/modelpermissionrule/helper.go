package modelpermissionrule

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/obot-platform/nah/pkg/backend"
	"github.com/obot-platform/obot/apiclient/types"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	kuser "k8s.io/apiserver/pkg/authentication/user"
	gocache "k8s.io/client-go/tools/cache"
)

const (
	mprUserIndex     = "user-ids"
	mprGroupIndex    = "group-ids"
	mprSelectorIndex = "selector-ids"
	dmaModelIndex    = "model-ids"
)

type Helper struct {
	mprIndexer, dmaIndexer gocache.Indexer
}

func NewHelper(ctx context.Context, backend backend.Backend) (*Helper, error) {
	// Create indexers for ModelPermissionRules and DefaultModelAliases
	mprGVK, err := backend.GroupVersionKindFor(&v1.ModelPermissionRule{})
	if err != nil {
		return nil, err
	}

	mprInformer, err := backend.GetInformerForKind(ctx, mprGVK)
	if err != nil {
		return nil, err
	}

	if err := mprInformer.AddIndexers(gocache.Indexers{
		mprUserIndex:     mprSubjectIndexFunc(types.SubjectTypeUser),
		mprGroupIndex:    mprSubjectIndexFunc(types.SubjectTypeGroup),
		mprSelectorIndex: mprSubjectIndexFunc(types.SubjectTypeSelector),
	}); err != nil {
		return nil, err
	}

	dmaGVK, err := backend.GroupVersionKindFor(&v1.DefaultModelAlias{})
	if err != nil {
		return nil, err
	}

	dmaInformer, err := backend.GetInformerForKind(ctx, dmaGVK)
	if err != nil {
		return nil, err
	}

	if err := dmaInformer.AddIndexers(gocache.Indexers{
		dmaModelIndex: dmaModelIndexFunc,
	}); err != nil {
		return nil, err
	}

	return &Helper{
		mprIndexer: mprInformer.GetIndexer(),
		dmaIndexer: dmaInformer.GetIndexer(),
	}, nil
}

// UserHasAccessToModel returns true if a user has access to a given model.
func (h *Helper) UserHasAccessToModel(user kuser.Info, modelID string) (bool, error) {
	if userIsAdminOrOwner(user) {
		return true, nil
	}

	// Check default models
	if slices.Contains(h.getDefaultModelIDs(), modelID) {
		return true, nil
	}

	// Check rules with wildcard subject selector (*) that include the model
	wildcardUserRules, err := h.getWildcardUserRules()
	if err != nil {
		return false, err
	}

	for _, rule := range wildcardUserRules {
		for _, model := range rule.Spec.Manifest.Models {
			if model.IsWildcard() || model.ID == modelID {
				return true, nil
			}
		}
	}

	// Check rules that the user is directly included in
	userID := user.GetUID()
	userRules, err := h.getUserRules(userID)
	if err != nil {
		return false, err
	}

	for _, rule := range userRules {
		for _, model := range rule.Spec.Manifest.Models {
			if model.IsWildcard() || model.ID == modelID {
				return true, nil
			}
		}
	}

	// Check rules based on group membership for each group the user belongs to
	for groupID := range authGroupSet(user) {
		groupRules, err := h.getGroupRules(groupID)
		if err != nil {
			return false, err
		}

		for _, rule := range groupRules {
			for _, model := range rule.Spec.Manifest.Models {
				if model.IsWildcard() || model.ID == modelID {
					return true, nil
				}
			}
		}
	}

	return false, nil
}

// GetAllowedModelsForUser returns all model IDs that a user has access to.
// Returns nil if the user has access to all models.
// There are three cases when a user has access to all models:
// 1. The user is an admin or owner
// 2. No model permission rules have been defined
func (h *Helper) GetAllowedModelsForUser(user kuser.Info) ([]string, bool, error) {
	if userIsAdminOrOwner(user) {
		// Admin/owner has access to all models
		return nil, true, nil
	}

	// Check rules with wildcard subject selector (*)
	allowedSet := make(map[string]struct{})
	wildcardUserRules, err := h.getWildcardUserRules()
	if err != nil {
		return nil, false, err
	}
	for _, rule := range wildcardUserRules {
		for _, model := range rule.Spec.Manifest.Models {
			if model.IsWildcard() {
				return nil, true, nil
			}
			allowedSet[model.ID] = struct{}{}
		}
	}

	// Check rules that the user is directly included in
	userID := user.GetUID()
	userRules, err := h.getUserRules(userID)
	if err != nil {
		return nil, false, err
	}

	for _, rule := range userRules {
		for _, model := range rule.Spec.Manifest.Models {
			if model.IsWildcard() {
				return nil, true, nil
			}
			allowedSet[model.ID] = struct{}{}
		}
	}

	// Check rules based on group membership
	for groupID := range authGroupSet(user) {
		groupRules, err := h.getGroupRules(groupID)
		if err != nil {
			return nil, false, err
		}

		for _, rule := range groupRules {
			for _, model := range rule.Spec.Manifest.Models {
				if model.IsWildcard() {
					return nil, true, nil
				}
				allowedSet[model.ID] = struct{}{}
			}
		}
	}

	// Add default models
	for _, id := range h.getDefaultModelIDs() {
		allowedSet[id] = struct{}{}
	}

	// Convert set to slice
	allowedModels := make([]string, 0, len(allowedSet))
	for modelID := range allowedSet {
		allowedModels = append(allowedModels, modelID)
	}

	return allowedModels, false, nil
}

// GetModelPermissionRulesForUser returns all ModelPermissionRules that apply to a specific user.
func (h *Helper) getUserRules(userID string) ([]v1.ModelPermissionRule, error) {
	return h.getIndexedRules(mprUserIndex, userID)
}

// getModelPermissionRulesForGroup returns all ModelPermissionRules that apply to given group.
func (h *Helper) getGroupRules(groupID string) ([]v1.ModelPermissionRule, error) {
	return h.getIndexedRules(mprGroupIndex, groupID)
}

// getAllUserRules returns all ModelPermissionRules that apply to all users.
func (h *Helper) getWildcardUserRules() ([]v1.ModelPermissionRule, error) {
	return h.getIndexedRules(mprSelectorIndex, "*")
}

// getIndexedRules returns all indexed ModelPermissionRules for a given index and key.
func (h *Helper) getIndexedRules(index, key string) ([]v1.ModelPermissionRule, error) {
	mprs, err := h.mprIndexer.ByIndex(index, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get model permission rules with wildcard subject: %w", err)
	}

	result := make([]v1.ModelPermissionRule, 0, len(mprs))
	for _, mpr := range mprs {
		if res, ok := mpr.(*v1.ModelPermissionRule); ok {
			result = append(result, *res)
		}
	}

	return result, nil
}

// getDefaultModelIDs returns the list of default model IDs.
func (h *Helper) getDefaultModelIDs() []string {
	return h.dmaIndexer.ListIndexFuncValues(dmaModelIndex)
}

// mprSubjectIndexFunc returns an index function that creates an index of ModelPermissionRule subject IDs for a given subject type
func mprSubjectIndexFunc(subjectType types.SubjectType) gocache.IndexFunc {
	return func(obj any) ([]string, error) {
		mpr := obj.(*v1.ModelPermissionRule)
		if !mpr.DeletionTimestamp.IsZero() {
			// Drop deleted objects from the index
			return nil, nil
		}

		var (
			subjects = mpr.Spec.Manifest.Subjects
			keys     = make([]string, 0, len(subjects))
		)
		for _, subject := range subjects {
			if subject.Type == subjectType {
				keys = append(keys, subject.ID)
			}
		}

		return keys, nil
	}
}

// dmaModelIndexFunc is an index function that creates an index of DefaultModelAlias model IDs.
func dmaModelIndexFunc(obj any) ([]string, error) {
	dma := obj.(*v1.DefaultModelAlias)
	if !dma.DeletionTimestamp.IsZero() {
		// Drop empty models and deleted objects from the index
		return nil, nil
	}

	model := dma.Spec.Manifest.Model
	if !strings.HasPrefix(model, system.ModelPrefix) {
		// Drop empty or invalid modelsIDs.
		return nil, nil
	}

	return []string{model}, nil
}

// authGroupSet returns a set of auth provider groups for a given user.
func authGroupSet(user kuser.Info) map[string]struct{} {
	groups := user.GetExtra()["auth_provider_groups"]
	set := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		set[group] = struct{}{}
	}
	return set
}

// userIsAdminOrOwner checks if the user is an admin or owner.
func userIsAdminOrOwner(user kuser.Info) bool {
	for _, group := range user.GetGroups() {
		switch group {
		case types.GroupAdmin, types.GroupOwner:
			return true
		}
	}
	return false
}
