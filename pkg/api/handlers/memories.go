package handlers

import (
	"slices"
	"strconv"
	"time"

	"github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	v1 "github.com/obot-platform/obot/pkg/storage/apis/obot.obot.ai/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type MemoryHandler struct {
}

func NewMemoryHandler() *MemoryHandler {
	return &MemoryHandler{}
}

// CreateMemory creates a new memory and responds with the created memory.
// If a memory with duplicate content already exists, this function no-ops and responds with the
// existing memory.
func (*MemoryHandler) CreateMemory(req api.Context) error {
	var memory types.Memory
	if err := req.Read(&memory); err != nil {
		return err
	}

	if unquoted, err := strconv.Unquote(memory.Content); err == nil {
		memory.Content = unquoted
	}

	if memory.Content == "" {
		return apierrors.NewBadRequest("content cannot be empty")
	}

	if memory.CreatedAt == nil || memory.CreatedAt.IsZero() {
		memory.CreatedAt = types.NewTime(time.Now())
	}

	thread, err := getThreadForScope(req)
	if err != nil {
		return err
	}

	var memorySet v1.MemorySet
	if err := req.Get(&memorySet, thread.Name); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}

		// MemorySet does not exist, create a new one with the given memory
		memorySet = v1.MemorySet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      thread.Name,
				Namespace: req.Namespace(),
			},
			Spec: v1.MemorySetSpec{
				ThreadName: thread.Name,
				Memories:   []types.Memory{memory},
			},
		}

		if err := req.Create(&memorySet); err != nil {
			return err
		}

		return req.Write(&memory)
	}

	if index := slices.IndexFunc(memorySet.Spec.Memories, func(m types.Memory) bool {
		return m.Content == memory.Content
	}); index >= 0 {
		// A memory with matching content already exists
		// Respond with the existing memory from the MemorySet to preserve the CreatedAt timestamp
		return req.Write(&memorySet.Spec.Memories[index])
	}

	// Memory with matching content doesn't exist, add it and update the MemorySet
	memorySet.Spec.Memories = append(memorySet.Spec.Memories, memory)
	if err := req.Update(&memorySet); err != nil {
		return err
	}

	return req.Write(&memory)
}

// ListMemories responds with a list containing all memories.
func (*MemoryHandler) ListMemories(req api.Context) error {
	thread, err := getThreadForScope(req)
	if err != nil {
		return err
	}

	var memorySet v1.MemorySet
	if err := req.Get(&memorySet, thread.Name); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return req.Write(&types.MemoryList{
		Items: memorySet.Spec.Memories,
	})
}

// DeleteMemories deletes one or all memories and responds with the memories that were deleted.
// If the memory_index path parameter is provided, the memory at that index is deleted.
// If no memory_index is provided, all memories are deleted.
func (*MemoryHandler) DeleteMemories(req api.Context) error {
	thread, err := getThreadForScope(req)
	if err != nil {
		return err
	}

	var memorySet v1.MemorySet
	if err := req.Get(&memorySet, thread.Name); err != nil {
		return err
	}

	memoryIndex := req.PathValue("memory_index")
	if memoryIndex == "" {
		// Delete all memories by deleting the MemorySet
		if err := req.Delete(&memorySet); err != nil {
			return err
		}

		// Respond with the list of deleted memories
		return req.Write(&types.MemoryList{
			Items: memorySet.Spec.Memories,
		})
	}

	// Delete the memory at the specified index
	index, err := strconv.Atoi(memoryIndex)
	if err != nil || index < 0 {
		return apierrors.NewBadRequest("invalid memory index")
	}

	if index >= len(memorySet.Spec.Memories) {
		return apierrors.NewNotFound(schema.GroupResource{}, memoryIndex)
	}

	deletedMemory := memorySet.Spec.Memories[index]
	memorySet.Spec.Memories = slices.Delete(memorySet.Spec.Memories, index, index+1)
	if err := req.Update(&memorySet); err != nil {
		return err
	}

	// Respond with the specific memory that was deleted
	return req.Write(&deletedMemory)
}

// UpdateMemory updates an existing memory at the specified index and responds with the updated memory.
// If a memory with the same content already exists, this function no-ops and responds with the existing
// memory.
func (*MemoryHandler) UpdateMemory(req api.Context) error {
	memoryIndex := req.PathValue("memory_index")
	if memoryIndex == "" {
		return apierrors.NewBadRequest("memory_index is required")
	}

	index, err := strconv.Atoi(memoryIndex)
	if err != nil || index < 0 {
		return apierrors.NewBadRequest("invalid memory index")
	}

	var memory types.Memory
	if err := req.Read(&memory); err != nil {
		return err
	}

	if unquoted, err := strconv.Unquote(memory.Content); err == nil {
		memory.Content = unquoted
	}

	if memory.Content == "" {
		return apierrors.NewBadRequest("memory content cannot be empty")
	}

	if memory.CreatedAt == nil || memory.CreatedAt.IsZero() {
		memory.CreatedAt = types.NewTime(time.Now())
	}

	thread, err := getThreadForScope(req)
	if err != nil {
		return err
	}

	var memorySet v1.MemorySet
	if err := req.Get(&memorySet, thread.Name); err != nil {
		return err
	}

	if index >= len(memorySet.Spec.Memories) {
		return apierrors.NewNotFound(schema.GroupResource{}, memoryIndex)
	}

	if existingMemory := memorySet.Spec.Memories[index]; existingMemory.Content == memory.Content {
		// Memory content is the same as the existing memory, respond with the existing memory
		return req.Write(&existingMemory)
	}

	memorySet.Spec.Memories[index] = memory
	if err := req.Update(&memorySet); err != nil {
		return err
	}

	return req.Write(&memory)
}
