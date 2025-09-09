package threads

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gptscript-ai/go-gptscript"
	"github.com/obot-platform/nah/pkg/name"
	"github.com/obot-platform/nah/pkg/randomtoken"
	"github.com/obot-platform/nah/pkg/router"
	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/create"
	"github.com/obot-platform/obot/pkg/invoke"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	"github.com/obot-platform/obot/pkg/system"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const templateSnapshotAnnotation = "obot.obot.ai/projectSnapshotRevision"

type Handler struct {
	gptScript *gptscript.GPTScript
	invoker   *invoke.Invoker
}

func NewHandler(gptScript *gptscript.GPTScript, invoker *invoke.Invoker) *Handler {
	return &Handler{gptScript: gptScript, invoker: invoker}
}

func (t *Handler) WorkflowState(req router.Request, _ router.Response) error {
	var (
		thread = req.Object.(*v1.Thread)
		wfe    v1.WorkflowExecution
	)

	if thread.Spec.WorkflowExecutionName != "" {
		if err := req.Get(&wfe, thread.Namespace, thread.Spec.WorkflowExecutionName); err != nil {
			return err
		}
		thread.Status.WorkflowState = wfe.Status.State
	}

	return nil
}

// SnapshotUpgradeStatus sets SnapshotUpgradeAvailable when a project created from a template has a newer template snapshot.
func (t *Handler) SnapshotUpgradeStatus(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if !thread.Spec.Project || thread.Spec.SourceThreadName == "" {
		return nil
	}

	var source v1.Thread
	if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, thread.Spec.SourceThreadName), &source); err != nil {
		return err
	}
	if !source.Spec.Template {
		return nil
	}

	srcRev := source.Annotations[templateSnapshotAnnotation]
	projRev := thread.Annotations[templateSnapshotAnnotation]
	desired := srcRev != "" && projRev != srcRev
	if thread.Status.SnapshotUpgradeAvailable != desired {
		thread.Status.SnapshotUpgradeAvailable = desired
		return req.Client.Status().Update(req.Ctx, thread)
	}
	return nil
}

func getParentWorkspaceNames(ctx context.Context, c kclient.Client, thread *v1.Thread) ([]string, bool, error) {
	var result []string

	if thread.Spec.Project {
		// Projects don't copy the parents/agent workspace unless it is a copy of another project
		if thread.Spec.SourceThreadName != "" {
			var sourceThread v1.Thread
			if err := c.Get(ctx, kclient.ObjectKey{Namespace: thread.Namespace, Name: thread.Spec.SourceThreadName}, &sourceThread); err != nil {
				return nil, false, err
			}
			if sourceThread.Status.WorkspaceName == "" {
				return nil, false, nil
			}
			return []string{sourceThread.Status.WorkspaceName}, true, nil
		}
		return nil, true, nil
	}

	parentThreadName := thread.Spec.ParentThreadName
	for parentThreadName != "" {
		var parentThread v1.Thread
		if err := c.Get(ctx, kclient.ObjectKey{Namespace: thread.Namespace, Name: parentThreadName}, &parentThread); err != nil {
			return nil, false, err
		}
		if !parentThread.Spec.Project {
			return nil, false, fmt.Errorf("parent thread %s is not a project", parentThreadName)
		}
		if !parentThread.Status.Created {
			return nil, false, nil
		}
		if parentThread.Status.WorkspaceName == "" {
			return nil, false, nil
		}
		result = append(result, parentThread.Status.WorkspaceName)
		parentThreadName = parentThread.Spec.ParentThreadName
	}

	if thread.Spec.AgentName != "" {
		var agent v1.Agent
		if err := c.Get(ctx, kclient.ObjectKey{Namespace: thread.Namespace, Name: thread.Spec.AgentName}, &agent); err != nil {
			return nil, false, err
		}
		if agent.Status.WorkspaceName == "" {
			// Waiting for the agent to be created
			return nil, false, nil
		}
		result = append(result, agent.Status.WorkspaceName)
	}

	slices.Reverse(result)
	return result, true, nil
}

func (t *Handler) CreateSharedWorkspace(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if thread.Status.SharedWorkspaceName != "" || !thread.IsProjectBased() {
		return nil
	}

	var parentThread v1.Thread
	if thread.IsUserThread() {
		if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, thread.Spec.ParentThreadName), &parentThread); err != nil {
			return err
		}
		if parentThread.Status.SharedWorkspaceName == "" {
			// Wait to be created
			return nil
		}

		thread.Status.SharedWorkspaceName = parentThread.Status.SharedWorkspaceName
		return req.Client.Status().Update(req.Ctx, thread)
	}

	if !thread.IsProjectThread() {
		// this should never be hit
		panic("only project threads can create local workspace")
	}

	ws := v1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    thread.Namespace,
			GenerateName: system.WorkspacePrefix,
			Finalizers:   []string{v1.WorkspaceFinalizer},
		},
		Spec: v1.WorkspaceSpec{
			ThreadName: thread.Name,
		},
	}

	if err := req.Client.Create(req.Ctx, &ws); err != nil {
		return err
	}

	thread.Status.SharedWorkspaceName = ws.Name
	return req.Client.Status().Update(req.Ctx, thread)
}

func getWorkspace(ctx context.Context, c kclient.WithWatch, thread *v1.Thread) (*v1.Workspace, error) {
	var ws v1.Workspace

	if thread.Spec.WorkspaceName != "" {
		return &ws, c.Get(ctx, kclient.ObjectKey{Namespace: thread.Namespace, Name: thread.Spec.WorkspaceName}, &ws)
	}

	if thread.Status.WorkspaceName != "" {
		return &ws, c.Get(ctx, kclient.ObjectKey{Namespace: thread.Namespace, Name: thread.Status.WorkspaceName}, &ws)
	}

	parents, ok, err := getParentWorkspaceNames(ctx, c, thread)
	if err != nil || !ok {
		return nil, err
	}

	ws = v1.Workspace{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    thread.Namespace,
			GenerateName: system.WorkspacePrefix,
			Finalizers:   []string{v1.WorkspaceFinalizer},
		},
		Spec: v1.WorkspaceSpec{
			ThreadName:         thread.Name,
			FromWorkspaceNames: parents,
		},
	}

	return &ws, c.Create(ctx, &ws)
}

func (t *Handler) CreateWorkspaces(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)

	ws, err := getWorkspace(req.Ctx, req.Client, thread)
	if err != nil || ws == nil {
		return err
	}

	var update bool
	if thread.Status.WorkspaceID != ws.Status.WorkspaceID {
		update = true
		thread.Status.WorkspaceID = ws.Status.WorkspaceID
	}
	if thread.Status.WorkspaceName != ws.Name {
		update = true
		thread.Status.WorkspaceName = ws.Name
	}
	if update {
		return req.Client.Status().Update(req.Ctx, thread)
	}
	return nil
}

func createKnowledgeSet(ctx context.Context, c kclient.Client, thread *v1.Thread, relatedKnowledgeSets []string, from string) (*v1.KnowledgeSet, error) {
	var ks = v1.KnowledgeSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    thread.Namespace,
			GenerateName: system.KnowledgeSetPrefix,
			Finalizers:   []string{v1.KnowledgeSetFinalizer},
		},
		Spec: v1.KnowledgeSetSpec{
			ThreadName:               thread.Name,
			RelatedKnowledgeSetNames: relatedKnowledgeSets,
			FromKnowledgeSetName:     from,
		},
	}

	return &ks, c.Create(ctx, &ks)
}

func (t *Handler) CreateKnowledgeSet(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if len(thread.Status.KnowledgeSetNames) > 0 || thread.Spec.AgentName == "" {
		return nil
	}

	var relatedKnowledgeSets []string
	var parentThreadName = thread.Spec.ParentThreadName

	if thread.Spec.SourceThreadName != "" {
		var sourceThread v1.Thread
		if err := req.Client.Get(req.Ctx, kclient.ObjectKey{Namespace: thread.Namespace, Name: thread.Spec.SourceThreadName}, &sourceThread); err != nil {
			return err
		}
		if sourceThread.Status.SharedKnowledgeSetName == "" {
			return nil
		}
		shared, err := createKnowledgeSet(req.Ctx, req.Client, thread, relatedKnowledgeSets, sourceThread.Status.SharedKnowledgeSetName)
		if err != nil {
			return err
		}

		thread.Status.SharedKnowledgeSetName = shared.Name
		if err := req.Client.Status().Update(req.Ctx, thread); err != nil {
			_ = req.Client.Delete(req.Ctx, shared)
			return err
		}
	} else {
		// Grab parents first so we have the list for the "related knowledge sets" if we need to create a new one
		for parentThreadName != "" {
			var parentThread v1.Thread
			if err := req.Client.Get(req.Ctx, kclient.ObjectKey{Namespace: thread.Namespace, Name: parentThreadName}, &parentThread); err != nil {
				return err
			}
			if !parentThread.Spec.Project {
				return fmt.Errorf("parent thread %s is not a project", parentThreadName)
			}
			if parentThread.Status.SharedKnowledgeSetName == "" {
				return nil
			}
			relatedKnowledgeSets = append(relatedKnowledgeSets, parentThread.Status.SharedKnowledgeSetName)
			parentThreadName = parentThread.Spec.ParentThreadName
		}
	}

	if thread.Status.SharedKnowledgeSetName == "" {
		shared, err := createKnowledgeSet(req.Ctx, req.Client, thread, relatedKnowledgeSets, "")
		if err != nil {
			return err
		}

		thread.Status.SharedKnowledgeSetName = shared.Name
		if err := req.Client.Status().Update(req.Ctx, thread); err != nil {
			_ = req.Client.Delete(req.Ctx, shared)
			return err
		}
	}

	relatedKnowledgeSets = append([]string{thread.Status.SharedKnowledgeSetName}, relatedKnowledgeSets...)
	thread.Status.KnowledgeSetNames = relatedKnowledgeSets
	return req.Client.Status().Update(req.Ctx, thread)
}

func (t *Handler) SetCreated(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if thread.Status.Created {
		return nil
	}

	if thread.Status.WorkspaceID == "" {
		return nil
	}

	if thread.IsProjectBased() && thread.Status.SharedWorkspaceName == "" {
		return nil
	}

	if thread.Spec.SourceThreadName != "" && len(thread.Spec.Manifest.SharedTasks) > 0 && !thread.Status.CopiedTasks {
		return nil
	}

	if thread.Spec.SourceThreadName != "" && !thread.Status.CopiedTools {
		return nil
	}

	if thread.Spec.AgentName == "" {
		// Non-agent thread is ready at this point
		thread.Status.Created = true
		return req.Client.Status().Update(req.Ctx, thread)
	}

	if thread.Status.SharedKnowledgeSetName == "" {
		return nil
	}

	if len(thread.Status.KnowledgeSetNames) == 0 {
		return nil
	}

	thread.Status.Created = true
	return req.Client.Update(req.Ctx, thread)
}

// EnsureTemplateThreadShare ensures a public ThreadShare exists for template threads
func (t *Handler) EnsureTemplateThreadShare(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if !thread.Spec.Template {
		return nil
	}
	// Create the share if it doesn't exist
	var share v1.ThreadShare
	if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, thread.Name), &share); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	publicID := strings.ReplaceAll(uuid.New().String(), "-", "")
	share = v1.ThreadShare{
		ObjectMeta: metav1.ObjectMeta{
			Name:      thread.Name,
			Namespace: thread.Namespace,
		},
		Spec: v1.ThreadShareSpec{
			UserID:            thread.Spec.UserID,
			ProjectThreadName: thread.Name,
			Template:          true,
			Featured:          false,
			Manifest:          types.ProjectShareManifest{Public: true},
			PublicID:          publicID,
		},
	}
	return req.Client.Create(req.Ctx, &share)
}

func (t *Handler) CleanupEphemeralThreads(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if !thread.Spec.Ephemeral ||
		thread.CreationTimestamp.After(time.Now().Add(-12*time.Hour)) {
		return nil
	}

	return kclient.IgnoreNotFound(req.Delete(thread))
}

func (t *Handler) GenerateName(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if !thread.IsUserThread() || thread.Spec.Manifest.Name != "" || thread.Status.LastRunName == "" ||
		thread.Spec.Ephemeral ||
		thread.Status.LastRunState != v1.Continue && thread.Status.LastRunState != v1.Waiting {
		return nil
	}

	var run v1.Run
	if err := req.Get(&run, thread.Namespace, thread.Status.LastRunName); err != nil {
		return err
	}

	result, err := t.invoker.EphemeralThreadTask(req.Ctx, thread, gptscript.ToolDef{
		Instructions: `Generate a concise (3 to 4 words) and descriptive thread name that encapsulates the main topic or theme of the following conversation starter. Do not enclose the title in quotes.`,
	}, fmt.Sprintf("User Input: %s\n\nLLM Response: %s", run.Spec.Input, run.Status.Output))
	if err != nil {
		return fmt.Errorf("failed to generate thread name: %w", err)
	}

	thread.Spec.Manifest.Name = strings.TrimSpace(result)
	return req.Client.Update(req.Ctx, thread)
}

func (t *Handler) EnsureShared(req router.Request, _ router.Response) error {
	wf := req.Object.(*v1.Workflow)
	if !wf.Spec.Managed {
		return nil
	}

	var sourceThread v1.Thread
	if err := req.Get(&sourceThread, wf.Namespace, wf.Spec.SourceThreadName); apierrors.IsNotFound(err) {
		return req.Delete(wf)
	} else if err != nil {
		return fmt.Errorf("failed to get source thread %s: %w", wf.Spec.SourceThreadName, err)
	}

	if !slices.Contains(sourceThread.Spec.Manifest.SharedTasks, wf.Spec.SourceWorkflowName) {
		return req.Delete(wf)
	}

	return nil
}

func (t *Handler) CopyTasksFromSource(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if !thread.Spec.Project || thread.Spec.SourceThreadName == "" || thread.Spec.ParentThreadName != "" {
		return nil
	}

	if thread.Status.CopiedTasks {
		return nil
	}

	var (
		modified     bool
		newTaskNames []string
		err          error
	)
	for _, taskName := range thread.Spec.Manifest.SharedTasks {
		var task v1.Workflow
		if err := req.Get(&task, thread.Namespace, taskName); apierrors.IsNotFound(err) {
			modified = true
			continue
		} else if err != nil {
			return err
		}
		if task.Spec.ThreadName == thread.Spec.SourceThreadName {
			modified = true
			newManifest := task.Spec.Manifest
			newManifest.Alias, err = randomtoken.Generate()
			if err != nil {
				return err
			}
			wf := v1.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: system.WorkflowPrefix,
					Namespace:    thread.Namespace,
				},
				Spec: v1.WorkflowSpec{
					ThreadName: thread.Name,
					Manifest:   newManifest,
				},
			}
			if err := req.Client.Create(req.Ctx, &wf); err != nil {
				return err
			}
			newTaskNames = append(newTaskNames, wf.Name)
		} else {
			newTaskNames = append(newTaskNames, taskName)
		}
	}

	if modified {
		thread.Spec.Manifest.SharedTasks = newTaskNames
		if err := req.Client.Update(req.Ctx, thread); err != nil {
			return err
		}
	}
	thread.Status.CopiedTasks = true
	return req.Client.Status().Update(req.Ctx, thread)
}

func (t *Handler) CopyToolsFromSource(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)
	if !thread.Spec.Project || thread.Spec.SourceThreadName == "" || thread.Spec.ParentThreadName != "" {
		return nil
	}

	if thread.Status.CopiedTools {
		return nil
	}

	var toolList v1.ToolList
	if err := req.Client.List(req.Ctx, &toolList, kclient.InNamespace(thread.Namespace), kclient.MatchingFields{
		"spec.threadName": thread.Spec.SourceThreadName,
	}); err != nil {
		return err
	}

	for _, tool := range toolList.Items {
		newTool := v1.Tool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name.SafeHashConcatName(tool.Name, thread.Name),
				Namespace: thread.Namespace,
			},
			Spec: v1.ToolSpec{
				ThreadName: thread.Name,
				Manifest:   tool.Spec.Manifest,
			},
		}
		if err := create.IfNotExists(req.Ctx, req.Client, &newTool); err != nil {
			return err
		}
	}

	var projectMcpList v1.ProjectMCPServerList
	if err := req.Client.List(req.Ctx, &projectMcpList, kclient.InNamespace(thread.Namespace), kclient.MatchingFields{
		"spec.threadName": thread.Spec.SourceThreadName,
	}); err != nil {
		return err
	}

	// Track desired ProjectMCPServer names for pruning obsolete ones later
	desiredPMSNames := map[string]struct{}{}

	for _, projectMcp := range projectMcpList.Items {
		// Determine the per-user target MCP backend to point to in the new project
		targetMCPID := projectMcp.Spec.Manifest.MCPID

		// Case 1: Source points to an MCPServerInstance -> create/update a per-thread instance pointing to the same server
		if system.IsMCPServerInstanceID(projectMcp.Spec.Manifest.MCPID) {
			var srcInst v1.MCPServerInstance
			if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, projectMcp.Spec.Manifest.MCPID), &srcInst); err != nil {
				return err
			}

			newInstName := name.SafeHashConcatName(system.MCPServerInstancePrefix, thread.Name, srcInst.Spec.MCPServerName)
			desiredInst := v1.MCPServerInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name:       newInstName,
					Namespace:  thread.Namespace,
					Finalizers: []string{v1.MCPServerInstanceFinalizer},
				},
				Spec: v1.MCPServerInstanceSpec{
					UserID:                    thread.Spec.UserID,
					MCPServerName:             srcInst.Spec.MCPServerName,
					MCPCatalogName:            srcInst.Spec.MCPCatalogName,
					MCPServerCatalogEntryName: srcInst.Spec.MCPServerCatalogEntryName,
				},
			}
			var existingInst v1.MCPServerInstance
			if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, newInstName), &existingInst); apierrors.IsNotFound(err) {
				if err := req.Client.Create(req.Ctx, &desiredInst); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				existingInst.Spec = desiredInst.Spec
				if err := req.Client.Update(req.Ctx, &existingInst); err != nil {
					return err
				}
			}
			targetMCPID = newInstName
		} else if system.IsMCPServerID(projectMcp.Spec.Manifest.MCPID) {
			// Case 2: Source points directly to an MCPServer
			var srcServer v1.MCPServer
			if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, projectMcp.Spec.Manifest.MCPID), &srcServer); err != nil {
				return err
			}

			if srcServer.Spec.SharedWithinMCPCatalogName != "" {
				// Shared (multi-user) server -> create/update a per-thread instance pointing to it
				newInstName := name.SafeHashConcatName(system.MCPServerInstancePrefix, thread.Name, srcServer.Name)
				desiredInst := v1.MCPServerInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:       newInstName,
						Namespace:  thread.Namespace,
						Finalizers: []string{v1.MCPServerInstanceFinalizer},
					},
					Spec: v1.MCPServerInstanceSpec{
						UserID:                    thread.Spec.UserID,
						MCPServerName:             srcServer.Name,
						MCPCatalogName:            srcServer.Spec.SharedWithinMCPCatalogName,
						MCPServerCatalogEntryName: srcServer.Spec.MCPServerCatalogEntryName,
					},
				}
				var existingInst v1.MCPServerInstance
				if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, newInstName), &existingInst); apierrors.IsNotFound(err) {
					if err := req.Client.Create(req.Ctx, &desiredInst); err != nil {
						return err
					}
				} else if err != nil {
					return err
				} else {
					existingInst.Spec = desiredInst.Spec
					if err := req.Client.Update(req.Ctx, &existingInst); err != nil {
						return err
					}
				}
				targetMCPID = newInstName
			} else {
				// Single-user server
				if srcServer.Spec.UserID == thread.Spec.UserID {
					// Already owned by this user; reuse it
					targetMCPID = srcServer.Name
				} else {
					// Clone a new per-user MCPServer by copying the manifest
					newServerName := name.SafeHashConcatName(system.MCPServerPrefix, thread.Name, srcServer.Name)
					desiredServer := v1.MCPServer{
						ObjectMeta: metav1.ObjectMeta{
							Name:       newServerName,
							Namespace:  thread.Namespace,
							Finalizers: []string{v1.MCPServerFinalizer},
						},
						Spec: v1.MCPServerSpec{
							Manifest:                   srcServer.Spec.Manifest,
							UnsupportedTools:           srcServer.Spec.UnsupportedTools,
							ThreadName:                 "",
							Alias:                      srcServer.Spec.Alias,
							UserID:                     thread.Spec.UserID,
							SharedWithinMCPCatalogName: "",
							MCPServerCatalogEntryName:  srcServer.Spec.MCPServerCatalogEntryName,
						},
					}
					var existingServer v1.MCPServer
					if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, newServerName), &existingServer); apierrors.IsNotFound(err) {
						if err := req.Client.Create(req.Ctx, &desiredServer); err != nil {
							return err
						}
					} else if err != nil {
						return err
					} else {
						existingServer.Spec = desiredServer.Spec
						if err := req.Client.Update(req.Ctx, &existingServer); err != nil {
							return err
						}
					}
					targetMCPID = newServerName
				}
			}
		}

		// Create the project-scoped mapping pointing to the new per-user backend
		newManifest := projectMcp.Spec.Manifest
		newManifest.MCPID = targetMCPID

		newPMSName := name.SafeHashConcatName(system.ProjectMCPServerPrefix, thread.Name, projectMcp.Name)
		desiredPMS := v1.ProjectMCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:       newPMSName,
				Namespace:  thread.Namespace,
				Finalizers: []string{v1.ProjectMCPServerFinalizer},
			},
			Spec: v1.ProjectMCPServerSpec{
				Manifest:   newManifest,
				ThreadName: thread.Name,
				UserID:     thread.Spec.UserID,
			},
		}
		var existingPMS v1.ProjectMCPServer
		if err := req.Client.Get(req.Ctx, router.Key(thread.Namespace, newPMSName), &existingPMS); apierrors.IsNotFound(err) {
			if err := req.Client.Create(req.Ctx, &desiredPMS); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			existingPMS.Spec = desiredPMS.Spec
			if err := req.Client.Update(req.Ctx, &existingPMS); err != nil {
				return err
			}
		}
		// Mark as desired
		desiredPMSNames[newPMSName] = struct{}{}
	}

	// Prune ProjectMCPServers that are no longer desired for this thread
	var existingPMSList v1.ProjectMCPServerList
	if err := req.Client.List(req.Ctx, &existingPMSList, kclient.InNamespace(thread.Namespace), kclient.MatchingFields{
		"spec.threadName": thread.Name,
	}); err != nil {
		return err
	}
	for _, pms := range existingPMSList.Items {
		if _, keep := desiredPMSNames[pms.Name]; !keep {
			if err := req.Client.Delete(req.Ctx, &pms); err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	thread.Status.CopiedTools = true
	return req.Client.Status().Update(req.Ctx, thread)
}

func (t *Handler) RemoveOldFinalizers(req router.Request, _ router.Response) error {
	thread := req.Object.(*v1.Thread)

	finalizerCount := len(thread.Finalizers)
	thread.Finalizers = slices.DeleteFunc(thread.Finalizers, func(finalizer string) bool {
		return finalizer == v1.ThreadFinalizer+"-child-cleanup" || finalizer == v1.MCPServerFinalizer
	})

	if finalizerCount != len(thread.Finalizers) {
		return req.Client.Update(req.Ctx, thread)
	}
	return nil
}
