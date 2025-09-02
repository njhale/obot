package handlers

import (
	"strings"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type TemplateHandler struct{}

func NewTemplateHandler() *TemplateHandler {
	return &TemplateHandler{}
}

func (h *TemplateHandler) CreateProjectTemplate(req api.Context) error {
	var (
		projectThread     v1.Thread
		projectID         = req.PathValue("project_id")
		projectThreadName = strings.Replace(projectID, system.ProjectPrefix, system.ThreadPrefix, 1)
	)

	if err := req.Get(&projectThread, projectThreadName); err != nil {
		return err
	}

	for projectThread.Spec.ParentThreadName != "" {
		if err := req.Get(&projectThread, projectThread.Spec.ParentThreadName); err != nil {
			return err
		}
	}

	if !projectThread.Spec.Project || projectThread.Spec.Template {
		return types.NewErrBadRequest("invalid project %s", projectID)
	}

	// Enforce one template per project: upsert existing template for this project
	var existingTemplateThreads v1.ThreadList
	if err := req.List(&existingTemplateThreads, kclient.InNamespace(projectThread.Namespace), kclient.MatchingFields{
		"spec.template":         "true",
		"spec.sourceThreadName": projectThread.Name,
	}, kclient.Limit(1)); err != nil {
		return err
	}
	if len(existingTemplateThreads.Items) > 0 {
		// Update the existing template's manifest/agent from the current project
		existing := existingTemplateThreads.Items[0]
		if !equality.Semantic.DeepEqual(existing.Spec.Manifest, projectThread.Spec.Manifest) {
			existing.Spec.Manifest = projectThread.Spec.Manifest
		}
		if existing.Spec.AgentName != projectThread.Spec.AgentName {
			existing.Spec.AgentName = projectThread.Spec.AgentName
		}

		// Always bump the snapshot annotation to current time
		if existing.Annotations == nil {
			existing.Annotations = map[string]string{}
		}
		existing.Annotations[templateSnapshotAnnotation] = time.Now().UTC().Format(time.RFC3339)

		if err := req.Update(&existing); err != nil {
			return err
		}

		// Reset copy status flags to trigger controller to re-sync derived resources
		existing.Status.CopiedTools = false
		existing.Status.CopiedTasks = false
		if err := req.Storage.Status().Update(req.Context(), &existing); err != nil {
			return err
		}

		return req.Write(convertTemplateThread(existing, nil))
	}

	templateThread := v1.Thread{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: system.ThreadPrefix,
			Namespace:    projectThread.Namespace,
			Annotations: map[string]string{
				templateSnapshotAnnotation: time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: v1.ThreadSpec{
			Manifest:         projectThread.Spec.Manifest,
			AgentName:        projectThread.Spec.AgentName,
			SourceThreadName: projectThread.Name,
			UserID:           projectThread.Spec.UserID,
			Project:          true,
			Template:         true,
		},
	}

	if err := req.Create(&templateThread); err != nil {
		return err
	}

	// ThreadShare creation is handled by the controller for template threads
	return req.WriteCreated(convertTemplateThread(templateThread, nil))
}

func (h *TemplateHandler) DeleteProjectTemplate(req api.Context) error {
	var (
		projectID         = req.PathValue("project_id")
		projectThreadName = strings.Replace(projectID, system.ProjectPrefix, system.ThreadPrefix, 1)
	)

	// Find the template thread that was created from this project
	var templateThreadList v1.ThreadList
	if err := req.List(&templateThreadList, kclient.MatchingFields{
		"spec.template":         "true",
		"spec.sourceThreadName": projectThreadName,
	}, kclient.Limit(1)); err != nil {
		return err
	}

	if len(templateThreadList.Items) == 0 {
		return types.NewErrNotFound("template not found for project %s", projectID)
	}

	templateThread := templateThreadList.Items[0]
	return req.Delete(&templateThread)
}

func (h *TemplateHandler) CopyTemplate(req api.Context) error {
	var (
		publicID          = req.PathValue("template_public_id")
		templateShareList v1.ThreadShareList
	)

	if err := req.List(&templateShareList, kclient.InNamespace(req.Namespace()), kclient.MatchingFields{
		"spec.publicID": publicID,
		"spec.template": "true",
	}, kclient.Limit(1)); err != nil {
		return err
	}

	if len(templateShareList.Items) < 1 {
		return types.NewErrNotFound("template not found: %s", publicID)
	}

	var templateThread v1.Thread
	if err := req.Get(&templateThread, templateShareList.Items[0].Spec.ProjectThreadName); err != nil {
		return err
	}

	newProject := v1.Thread{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: system.ThreadPrefix,
			Namespace:    req.Namespace(),
			Annotations:  map[string]string{},
		},
		Spec: v1.ThreadSpec{
			Manifest:         templateThread.Spec.Manifest,
			AgentName:        templateThread.Spec.AgentName,
			SourceThreadName: templateThread.Name,
			UserID:           req.User.GetUID(),
			Project:          true,
		},
	}

	// Copy snapshot revision annotation from source template if present
	if ts := templateThread.Annotations[templateSnapshotAnnotation]; ts != "" {
		newProject.Annotations[templateSnapshotAnnotation] = ts
	}

	if err := req.Create(&newProject); err != nil {
		return err
	}

	return req.Write(convertProject(&newProject, nil))
}

func (h *TemplateHandler) GetProjectTemplate(req api.Context) error {
	var (
		projectID         = req.PathValue("project_id")
		projectThreadName = strings.Replace(projectID, system.ProjectPrefix, system.ThreadPrefix, 1)
	)

	// Find the template thread that was created from this project
	var templateThreadList v1.ThreadList
	if err := req.List(&templateThreadList, kclient.MatchingFields{
		"spec.template":         "true",
		"spec.sourceThreadName": projectThreadName,
	}, kclient.Limit(1)); err != nil {
		return err
	}

	if len(templateThreadList.Items) == 0 {
		return types.NewErrNotFound("template not found for project %s", projectID)
	}

	templateThread := templateThreadList.Items[0]

	var templateShareList v1.ThreadShareList
	if err := req.List(&templateShareList, kclient.MatchingFields{
		"spec.template":          "true",
		"spec.projectThreadName": templateThread.Name,
	}, kclient.Limit(1)); err != nil {
		return err
	}

	var templateShare *v1.ThreadShare
	if len(templateShareList.Items) > 0 {
		templateShare = &templateShareList.Items[0]
	}

	return req.Write(convertTemplateThread(templateThread, templateShare))
}

func (h *TemplateHandler) GetTemplate(req api.Context) error {
	var (
		publicID          = req.PathValue("template_public_id")
		templateShareList v1.ThreadShareList
	)

	if err := req.List(&templateShareList, kclient.InNamespace(req.Namespace()), kclient.MatchingFields{
		"spec.publicID": publicID,
		"spec.template": "true",
	}, kclient.Limit(1)); err != nil {
		return err
	}

	if len(templateShareList.Items) < 1 {
		return types.NewErrNotFound("template not found: %s", publicID)
	}

	var templateThread v1.Thread
	if err := req.Get(&templateThread, templateShareList.Items[0].Spec.ProjectThreadName); err != nil {
		return err
	}

	return req.Write(convertTemplateThread(templateThread, &templateShareList.Items[0]))
}
