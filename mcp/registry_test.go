package mcp

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryRegisterList(t *testing.T) {
	registry := NewRegistry()
	tool := &stubTool{
		name:        "CreateModel",
		description: "create model",
		schema:      `{"name":"CreateModel","description":"create model","inputSchema":{"type":"object"},"outputSchema":{"type":"object"}}`,
	}

	require.NoError(t, registry.Register(tool))

	definitions := registry.Definitions()
	require.Len(t, definitions, 1)
	assert.Equal(t, "CreateModel", definitions[0].Name)
	assert.NotEmpty(t, definitions[0].InputSchema)
	assert.NotEmpty(t, definitions[0].OutputSchema)
}

func TestRegistryRegisterInvalidSchema(t *testing.T) {
	registry := NewRegistry()
	tool := &stubTool{
		name:   "BadTool",
		schema: `{"name":`,
	}

	require.Error(t, registry.Register(tool))
}

func TestRegistryRegisterNilTool(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil tool")
}

func TestRegistryRegisterMissingName(t *testing.T) {
	registry := NewRegistry()
	tool := &stubTool{
		name:   "NoName",
		schema: `{"name":"","description":"missing name","inputSchema":{"type":"object"}}`,
	}
	err := registry.Register(tool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing tool name")
}

func TestRegistryRegisterMissingInputSchema(t *testing.T) {
	registry := NewRegistry()
	tool := &stubTool{
		name:   "NoInputSchema",
		schema: `{"name":"NoInputSchema","description":"no input schema"}`,
	}
	err := registry.Register(tool)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing input schema")
}

func TestRegistryReregister(t *testing.T) {
	registry := NewRegistry()

	tool1 := &stubTool{
		name:        "Tool",
		description: "first version",
		schema:      `{"name":"Tool","description":"first version","inputSchema":{"type":"object"}}`,
	}
	tool2 := &stubTool{
		name:        "Tool",
		description: "second version",
		schema:      `{"name":"Tool","description":"second version","inputSchema":{"type":"object"}}`,
	}

	require.NoError(t, registry.Register(tool1))
	require.NoError(t, registry.Register(tool2))

	definitions := registry.Definitions()
	require.Len(t, definitions, 1, "re-registering should not create duplicates")
	assert.Equal(t, "second version", definitions[0].Description)
}

func TestRegistryConcurrentAccess(t *testing.T) {
	registry := NewRegistry()
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent Register calls
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			tool := &stubTool{
				name:   fmt.Sprintf("Tool%d", n),
				schema: fmt.Sprintf(`{"name":"Tool%d","description":"tool %d","inputSchema":{"type":"object"}}`, n, n),
			}
			_ = registry.Register(tool)
		}(i)
	}

	// Concurrent Definitions calls
	for range goroutines {
		go func() {
			defer wg.Done()
			_ = registry.Definitions()
		}()
	}

	// Concurrent Get calls
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			_, _ = registry.Get(fmt.Sprintf("Tool%d", n))
		}(i)
	}

	wg.Wait()

	// All 100 tools should be registered
	defs := registry.Definitions()
	assert.Len(t, defs, goroutines)
}
