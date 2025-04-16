package handlers

import (
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	"github.com/obot-platform/obot/pkg/hash"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type MemorySetHandler struct {
	client kclient.Client
}

func NewMemorySetHandler(client kclient.Client) *MemorySetHandler {
	return &MemorySetHandler{
		client: client,
	}
}

func (h *MemorySetHandler) AddMemories(req api.Context) error {
	var (
		memorySet v1.MemorySet
		memories  []types.Memory
	)

	// TODO(njhale): Use project_id and not the thread for scope?
	thread, err := getThreadForScope(req)
	if err != nil {
		return err
	}

	if err := req.Read(&memories); err != nil {
		return err
	}

	// Current time for new memories
	currentTime := types.NewTime(time.Now())

	// Assign IDs based on content hash and deduplicate
	newMemories := make(map[string]types.Memory)
	for i := range memories {
		memory := memories[i]
		// Generate ID from content hash if not provided
		if memory.ID == "" {
			// Create a shorter URL-friendly hash (first 12 chars of SHA-256)
			fullHash := hash.String(memory.Content)
			memory.ID = fullHash[:12]
		}
		// Always set creation time to current time
		memory.CreatedAt = *currentTime
		newMemories[memory.ID] = memory
	}

	if err := req.Get(&memorySet, thread.Name); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		// Create a new memory set with the memories
		memorySet = v1.MemorySet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      thread.Name,
				Namespace: req.Namespace(),
			},
			Spec: v1.MemorySetSpec{
				ThreadName: thread.Name,
				Manifest: types.MemorySetManifest{
					Memories: []types.Memory{},
				},
			},
		}

		// Add all memories from the map
		for _, memory := range newMemories {
			memorySet.Spec.Manifest.Memories = append(memorySet.Spec.Manifest.Memories, memory)
		}

		if err := req.Create(&memorySet); err != nil {
			return err
		}
	} else {
		// Create a map of existing memories for easy lookup
		existingMap := make(map[string]struct{})
		for _, memory := range memorySet.Spec.Manifest.Memories {
			existingMap[memory.ID] = struct{}{}
		}

		// Update existing entries or add new ones
		for id, memory := range newMemories {
			if _, exists := existingMap[id]; exists {
				continue
			}

			// Add new memory
			memorySet.Spec.Manifest.Memories = append(memorySet.Spec.Manifest.Memories, memory)
		}

		if err := req.Update(&memorySet); err != nil {
			return err
		}
	}

	// Convert the map back to a list for the response
	return req.Write(&types.MemorySet{
		Metadata: types.Metadata{
			ID: memorySet.Name,
		},
		MemorySetManifest: memorySet.Spec.Manifest,
	})
}

func (h *MemorySetHandler) GetMemories(req api.Context) error {
	var memorySet v1.MemorySet
	thread, err := getThreadForScope(req)
	if err != nil {
		return err
	}

	if err := req.Get(&memorySet, thread.Name); err != nil {
		return err
	}

	return req.Write(&types.MemorySet{
		Metadata: types.Metadata{
			ID: memorySet.Name,
		},
		MemorySetManifest: memorySet.Spec.Manifest,
	})
}

func (h *MemorySetHandler) DeleteMemories(req api.Context) error {
	thread, err := getThreadForScope(req)
	if err != nil {
		return err
	}

	if err := req.Delete(&v1.MemorySet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      thread.Name,
			Namespace: req.Namespace(),
		},
	}); err != nil {
		return err
	}

	return nil
}
