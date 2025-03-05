package handlers

import (
	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/storage/selectors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ProviderTriggerHandler struct{}

func NewProviderTriggerHandler() *ProviderTriggerHandler {
	return &ProviderTriggerHandler{}
}

func (h *ProviderTriggerHandler) Update(req api.Context) error {
	var (
		id = req.PathValue("id")
		dt v1.ProviderTrigger
	)

	if err := req.Get(&dt, id); err != nil {
		return err
	}

	var manifest types.ProviderTriggerManifest
	if err := req.Read(&manifest); err != nil {
		return err
	}

	if err := h.validateManifest(req, manifest); err != nil {
		return err
	}

	dt.Spec.ProviderTriggerManifest = manifest
	if err := req.Update(&dt); err != nil {
		return err
	}

	return req.Write(h.convert(dt))
}

func (*ProviderTriggerHandler) Delete(req api.Context) error {
	return req.Delete(&v1.ProviderTrigger{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.PathValue("id"),
			Namespace: req.Namespace(),
		},
	})
}

func (h *ProviderTriggerHandler) ByID(req api.Context) error {
	var (
		dt v1.ProviderTrigger
		id = req.PathValue("id")
	)

	if err := req.Get(&dt, id); err != nil {
		return err
	}

	return req.Write(h.convert(dt))
}

func (h *ProviderTriggerHandler) List(req api.Context) error {
	var providerTriggers v1.ProviderTriggerList
	if err := req.List(&providerTriggers, &client.ListOptions{
		FieldSelector: fields.SelectorFromSet(selectors.RemoveEmpty(map[string]string{
			"spec.provider": req.URL.Query().Get("provider"),
		})),
	}); err != nil {
		return err
	}

	var resp types.ProviderTriggerList
	for _, dt := range providerTriggers.Items {
		resp.Items = append(resp.Items, *h.convert(dt))
	}

	return req.Write(resp)
}

func (*ProviderTriggerHandler) validateManifest(req api.Context, manifest types.ProviderTriggerManifest) error {
	// TODO(njhale): Validate the  trigger provider exists and the options on the manifest
	if manifest.Provider == "" {
		return apierrors.NewBadRequest("provider trigger manifest must specify a provider")
	}

	var ref v1.ToolReference
	if err := req.Get(&ref, manifest.Provider); err != nil {
		return types.NewErrBadRequest("failed to get provider trigger provider %q: %s", manifest.Provider, err.Error())
	}
	if ref.Spec.Type != types.ToolReferenceTypeTriggerProvider {
		return types.NewErrBadRequest("%q is not a trigger provider", manifest.Provider)
	}

	// TODO(njhale): Check if  trigger provider is configured
	// TODO(njhale): Validate configured options for trigger against provider

	return nil
}

func (*ProviderTriggerHandler) convert(internal v1.ProviderTrigger) *types.ProviderTrigger {
	manifest := internal.Spec.ProviderTriggerManifest
	external := &types.ProviderTrigger{
		Metadata:                MetadataFrom(&internal),
		TaskID:                  internal.Spec.Workflow,
		ProviderTriggerManifest: manifest,
	}
	return external
}
