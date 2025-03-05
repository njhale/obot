package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gptscript-ai/go-gptscript"
	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/api/handlers/providers"
	"github.com/obot-platform/obot/pkg/gateway/server/dispatcher"
	"github.com/obot-platform/obot/pkg/invoke"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type TriggerProviderHandler struct {
	gptscript  *gptscript.GPTScript
	dispatcher *dispatcher.Dispatcher
	invoker    *invoke.Invoker
}

func NewTriggerProviderHandler(gClient *gptscript.GPTScript, dispatcher *dispatcher.Dispatcher, invoker *invoke.Invoker) *TriggerProviderHandler {
	return &TriggerProviderHandler{
		gptscript:  gClient,
		dispatcher: dispatcher,
		invoker:    invoker,
	}
}

func (h *TriggerProviderHandler) ByID(req api.Context) error {
	var ref v1.ToolReference
	if err := req.Get(&ref, req.PathValue("id")); err != nil {
		return err
	}

	if ref.Spec.Type != types.ToolReferenceTypeTriggerProvider {
		return types.NewErrNotFound(
			"trigger provider %q not found",
			ref.Name,
		)
	}

	var credEnvVars map[string]string
	if ref.Status.Tool != nil {
		aps, err := providers.ConvertTriggerProviderToolRef(ref, nil)
		if err != nil {
			return err
		}
		if len(aps.RequiredConfigurationParameters) > 0 {
			cred, err := h.gptscript.RevealCredential(req.Context(), []string{string(ref.UID), system.GenericTriggerProviderCredentialContext}, ref.Name)
			if err != nil && !errors.As(err, &gptscript.ErrNotFound{}) {
				return fmt.Errorf("failed to reveal credential for trigger provider %q: %w", ref.Name, err)
			} else if err == nil {
				credEnvVars = cred.Env
			}
		}
	}

	triggerProvider, err := convertToolReferenceToTriggerProvider(ref, credEnvVars)
	if err != nil {
		return err
	}

	return req.Write(triggerProvider)
}

func (h *TriggerProviderHandler) List(req api.Context) error {
	var refList v1.ToolReferenceList
	if err := req.List(&refList, &kclient.ListOptions{
		Namespace: req.Namespace(),
		FieldSelector: fields.SelectorFromSet(map[string]string{
			"spec.type": string(types.ToolReferenceTypeTriggerProvider),
		}),
	}); err != nil {
		return err
	}

	credCtxs := make([]string, 0, len(refList.Items)+1)
	for _, ref := range refList.Items {
		credCtxs = append(credCtxs, string(ref.UID))
	}
	credCtxs = append(credCtxs, system.GenericTriggerProviderCredentialContext)

	creds, err := h.gptscript.ListCredentials(req.Context(), gptscript.ListCredentialsOptions{
		CredentialContexts: credCtxs,
	})
	if err != nil {
		return fmt.Errorf("failed to list trigger provider credentials: %w", err)
	}

	credMap := make(map[string]map[string]string, len(creds))
	for _, cred := range creds {
		credMap[cred.Context+cred.ToolName] = cred.Env
	}

	resp := make([]types.TriggerProvider, 0, len(refList.Items))
	for _, ref := range refList.Items {
		env, ok := credMap[string(ref.UID)+ref.Name]
		if !ok {
			env = credMap[system.GenericTriggerProviderCredentialContext+ref.Name]
		}
		triggerProvider, err := convertToolReferenceToTriggerProvider(ref, env)
		if err != nil {
			log.Warnf("failed to convert trigger provider %q: %v", ref.Name, err)
			continue
		}
		resp = append(resp, triggerProvider)
	}

	return req.Write(types.TriggerProviderList{Items: resp})
}

func (h *TriggerProviderHandler) Configure(req api.Context) error {
	var ref v1.ToolReference
	if err := req.Get(&ref, req.PathValue("id")); err != nil {
		return err
	}

	if ref.Spec.Type != types.ToolReferenceTypeTriggerProvider {
		return types.NewErrBadRequest("%q is not an trigger provider", ref.Name)
	}

	var envVars map[string]string
	if err := req.Read(&envVars); err != nil {
		return err
	}

	// Allow for updating credentials. The only way to update a credential is to delete the existing one and recreate it.
	cred, err := h.gptscript.RevealCredential(req.Context(), []string{string(ref.UID), system.GenericTriggerProviderCredentialContext}, ref.Name)
	if err != nil {
		if !errors.As(err, &gptscript.ErrNotFound{}) {
			return fmt.Errorf("failed to find credential: %w", err)
		}
	} else if err = h.gptscript.DeleteCredential(req.Context(), cred.Context, ref.Name); err != nil {
		return fmt.Errorf("failed to remove existing credential: %w", err)
	}

	for key, val := range envVars {
		if val == "" {
			delete(envVars, key)
		}
	}

	if err := h.gptscript.CreateCredential(req.Context(), gptscript.Credential{
		Context:  string(ref.UID),
		ToolName: ref.Name,
		Type:     gptscript.CredentialTypeTool,
		Env:      envVars,
	}); err != nil {
		return fmt.Errorf("failed to create credential for trigger provider %q: %w", ref.Name, err)
	}

	h.dispatcher.StopTriggerProvider(ref.Namespace, ref.Name)

	if ref.Annotations[v1.TriggerProviderSyncAnnotation] == "" {
		if ref.Annotations == nil {
			ref.Annotations = make(map[string]string, 1)
		}
		ref.Annotations[v1.TriggerProviderSyncAnnotation] = "true"
	} else {
		delete(ref.Annotations, v1.TriggerProviderSyncAnnotation)
	}

	return req.Update(&ref)
}

type TriggerOptions struct {
	Options string `json:"options,omitempty"`
	Err     string `json:"error,omitempty"`
}

func (o *TriggerOptions) Error() string {
	return fmt.Sprintf("failed to get trigger options: {\"error\": \"%s\"}", o.Err)
}

func (h *TriggerProviderHandler) Options(req api.Context) error {
	var ref v1.ToolReference
	if err := req.Get(&ref, req.PathValue("id")); err != nil {
		return err
	}

	if ref.Spec.Type != types.ToolReferenceTypeTriggerProvider {
		return types.NewErrBadRequest("%q is not a trigger provider", ref.Name)
	}

	log.Debugf("Getting options for trigger provider %q", ref.Name)

	data, err := req.Body()
	if err != nil {
		return err
	}
	envs := []string{string(data)}

	thread := &v1.Thread{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: system.ThreadPrefix + "-" + ref.Name + "-options-",
			Namespace:    ref.Namespace,
		},
		Spec: v1.ThreadSpec{
			SystemTask: true,
		},
	}

	if err := req.Create(thread); err != nil {
		return fmt.Errorf("failed to create thread: %w", err)
	}

	defer func() { _ = req.Delete(thread) }()

	task, err := h.invoker.SystemTask(req.Context(), thread, "options from "+ref.Spec.Reference, "", invoke.SystemTaskOptions{Env: envs})
	if err != nil {
		return err
	}
	defer task.Close()

	res, err := task.Result(req.Context())
	if err != nil {
		if strings.Contains(err.Error(), "tool not found: options from "+ref.Spec.Reference) { // there's no simple way to do errors.As/.Is at this point unfortunately
			log.Errorf("Trigger provider %q does not provide an options tool. Looking for 'validate from %s'", ref.Name, ref.Spec.Reference)
			return types.NewErrNotFound(
				fmt.Sprintf("`options from %s` tool not found", ref.Spec.Reference),
				ref.Name,
			)
		}
		return types.NewErrHTTP(http.StatusUnprocessableEntity, strings.Trim(err.Error(), "\"'"))
	}

	var triggerOptions TriggerOptions
	if json.Unmarshal([]byte(res.Output), &triggerOptions) == nil && triggerOptions.Err != "" {
		return types.NewErrHTTP(http.StatusUnprocessableEntity, triggerOptions.Error())
	}

	return req.Write(triggerOptions)
}

func (h *TriggerProviderHandler) Deconfigure(req api.Context) error {
	var ref v1.ToolReference
	if err := req.Get(&ref, req.PathValue("id")); err != nil {
		return err
	}

	if ref.Spec.Type != types.ToolReferenceTypeTriggerProvider {
		return types.NewErrBadRequest("%q is not a trigger provider", ref.Name)
	}

	cred, err := h.gptscript.RevealCredential(req.Context(), []string{string(ref.UID), system.GenericTriggerProviderCredentialContext}, ref.Name)
	if err != nil {
		if !errors.As(err, &gptscript.ErrNotFound{}) {
			return fmt.Errorf("failed to find credential: %w", err)
		}
	} else if err = h.gptscript.DeleteCredential(req.Context(), cred.Context, ref.Name); err != nil {
		return fmt.Errorf("failed to remove existing credential: %w", err)
	}

	// Stop the trigger provider so that the credential is completely removed from the system.
	h.dispatcher.StopTriggerProvider(ref.Namespace, ref.Name)

	if ref.Annotations[v1.TriggerProviderSyncAnnotation] == "" {
		if ref.Annotations == nil {
			ref.Annotations = make(map[string]string, 1)
		}
		ref.Annotations[v1.TriggerProviderSyncAnnotation] = "true"
	} else {
		delete(ref.Annotations, v1.TriggerProviderSyncAnnotation)
	}

	return req.Update(&ref)
}

func (h *TriggerProviderHandler) Reveal(req api.Context) error {
	var ref v1.ToolReference
	if err := req.Get(&ref, req.PathValue("id")); err != nil {
		return err
	}

	if ref.Spec.Type != types.ToolReferenceTypeTriggerProvider {
		return types.NewErrBadRequest("%q is not a trigger provider", ref.Name)
	}

	cred, err := h.gptscript.RevealCredential(req.Context(), []string{string(ref.UID), system.GenericTriggerProviderCredentialContext}, ref.Name)
	if err != nil && !errors.As(err, &gptscript.ErrNotFound{}) {
		return fmt.Errorf("failed to reveal credential for trigger provider %q: %w", ref.Name, err)
	} else if err == nil {
		return req.Write(cred.Env)
	}

	return types.NewErrNotFound("no credential found for %q", ref.Name)
}

func convertToolReferenceToTriggerProvider(ref v1.ToolReference, credEnvVars map[string]string) (types.TriggerProvider, error) {
	name := ref.Name
	if ref.Status.Tool != nil {
		name = ref.Status.Tool.Name
	}

	tps, err := providers.ConvertTriggerProviderToolRef(ref, credEnvVars)
	if err != nil {
		return types.TriggerProvider{}, err
	}
	tp := types.TriggerProvider{
		Metadata: MetadataFrom(&ref),
		TriggerProviderManifest: types.TriggerProviderManifest{
			Name:          name,
			Namespace:     ref.Namespace,
			ToolReference: ref.Spec.Reference,
		},
		TriggerProviderStatus: *tps,
	}

	tp.Type = "triggerprovider"

	return tp, nil
}
