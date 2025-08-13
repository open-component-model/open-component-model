package sync

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestMap_StoreAndLoad(t *testing.T) {
	m := &Map[string, int]{}

	// Test basic store and load
	m.Store("key1", 42)
	value, ok := m.Load("key1")
	if !ok {
		t.Error("Expected key1 to be found")
	}
	if value != 42 {
		t.Errorf("Expected value 42, got %d", value)
	}

	// Test loading non-existent key
	_, ok = m.Load("nonexistent")
	if ok {
		t.Error("Expected nonexistent key to not be found")
	}

	// Test overwriting existing key
	m.Store("key1", 100)
	value, ok = m.Load("key1")
	if !ok {
		t.Error("Expected key1 to be found after overwrite")
	}
	if value != 100 {
		t.Errorf("Expected value 100, got %d", value)
	}
}

func TestMap_Delete(t *testing.T) {
	m := &Map[string, int]{}

	// Store a value
	m.Store("key1", 42)

	// Verify it exists
	value, ok := m.Load("key1")
	if !ok || value != 42 {
		t.Error("Expected key1 to exist with value 42")
	}

	// Delete the value
	m.Delete("key1")

	// Verify it's gone
	_, ok = m.Load("key1")
	if ok {
		t.Error("Expected key1 to be deleted")
	}

	// Test deleting non-existent key (should not panic)
	m.Delete("nonexistent")
}

func TestMap_LoadAndDelete(t *testing.T) {
	m := &Map[string, int]{}

	// Test LoadAndDelete on non-existent key
	value, loaded := m.LoadAndDelete("nonexistent")
	if loaded {
		t.Error("Expected LoadAndDelete to return loaded=false for non-existent key")
	}
	if value != 0 {
		t.Errorf("Expected zero value for non-existent key, got %d", value)
	}

	// Store a value
	m.Store("key1", 42)

	// LoadAndDelete existing key
	value, loaded = m.LoadAndDelete("key1")
	if !loaded {
		t.Error("Expected LoadAndDelete to return loaded=true for existing key")
	}
	if value != 42 {
		t.Errorf("Expected value 42, got %d", value)
	}

	// Verify key is deleted
	_, ok := m.Load("key1")
	if ok {
		t.Error("Expected key1 to be deleted after LoadAndDelete")
	}
}

func TestMap_LoadOrStore(t *testing.T) {
	m := &Map[string, int]{}

	// Test LoadOrStore on non-existent key
	value, loaded := m.LoadOrStore("key1", 42)
	if loaded {
		t.Error("Expected LoadOrStore to return loaded=false for new key")
	}
	if value != 42 {
		t.Errorf("Expected value 42, got %d", value)
	}

	// Verify the value was stored
	storedValue, ok := m.Load("key1")
	if !ok || storedValue != 42 {
		t.Error("Expected key1 to be stored with value 42")
	}

	// Test LoadOrStore on existing key
	value, loaded = m.LoadOrStore("key1", 100)
	if !loaded {
		t.Error("Expected LoadOrStore to return loaded=true for existing key")
	}
	if value != 42 {
		t.Errorf("Expected existing value 42, got %d", value)
	}

	// Verify the original value is still there
	storedValue, ok = m.Load("key1")
	if !ok || storedValue != 42 {
		t.Error("Expected key1 to still have original value 42")
	}
}

func TestMap_Range(t *testing.T) {
	m := &Map[string, int]{}

	// Add some test data
	testData := map[string]int{
		"key1": 1,
		"key2": 2,
		"key3": 3,
	}

	for k, v := range testData {
		m.Store(k, v)
	}

	// Test Range
	visited := make(map[string]int)
	m.Range(func(key string, value int) bool {
		visited[key] = value
		return true // continue iteration
	})

	// Verify all items were visited
	if len(visited) != len(testData) {
		t.Errorf("Expected %d items to be visited, got %d", len(testData), len(visited))
	}

	for k, v := range testData {
		if visited[k] != v {
			t.Errorf("Expected key %s to have value %d, got %d", k, v, visited[k])
		}
	}

	// Test Range with early termination
	visited = make(map[string]int)
	m.Range(func(key string, value int) bool {
		visited[key] = value
		return false // stop iteration after first item
	})

	if len(visited) != 1 {
		t.Errorf("Expected 1 item to be visited with early termination, got %d", len(visited))
	}
}

func TestMap_ConcurrentAccess(t *testing.T) {
	m := &Map[int, string]{}
	const numGoroutines = 10
	const numOperations = 1000

	var wg sync.WaitGroup

	// StartRenderLoop multiple goroutines that read and write concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := id*numOperations + j
				value := "value-" + strconv.Itoa(key)

				// Store
				m.Store(key, value)

				// Load
				loadedValue, ok := m.Load(key)
				if !ok {
					t.Errorf("Failed to load key %d", key)
				}
				if loadedValue != value {
					t.Errorf("Expected value %s for key %d, got %s", value, key, loadedValue)
				}

				// LoadOrStore
				_, loaded := m.LoadOrStore(key, "different-value")
				if !loaded {
					t.Errorf("Expected LoadOrStore to return loaded=true for existing key %d", key)
				}

				// LoadAndDelete occasionally
				if j%10 == 0 {
					_, loaded := m.LoadAndDelete(key)
					if !loaded {
						t.Errorf("Expected LoadAndDelete to return loaded=true for existing key %d", key)
					}
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestMap_TypeSafety(t *testing.T) {
	// Test with different types
	stringMap := &Map[string, int]{}
	intMap := &Map[int, string]{}
	floatMap := &Map[float64, bool]{}

	// String key, int value
	stringMap.Store("hello", 42)
	value, ok := stringMap.Load("hello")
	if !ok || value != 42 {
		t.Error("String key map failed")
	}

	// Int key, string value
	intMap.Store(123, "world")
	value2, ok := intMap.Load(123)
	if !ok || value2 != "world" {
		t.Error("Int key map failed")
	}

	// Float key, bool value
	floatMap.Store(3.14, true)
	value3, ok := floatMap.Load(3.14)
	if !ok || !value3 {
		t.Error("Float key map failed")
	}
}

func TestMap_StressTest(t *testing.T) {
	m := &Map[int, string]{}
	const numOperations = 10000

	// Concurrent stress test
	var wg sync.WaitGroup
	const numWriters = 5
	const numReaders = 10

	// StartRenderLoop writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := writerID*numOperations + j
				value := "writer-" + strconv.Itoa(writerID) + "-value-" + strconv.Itoa(j)
				m.Store(key, value)
			}
		}(i)
	}

	// StartRenderLoop readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := j % (numWriters * numOperations)
				m.Load(key)
			}
		}(i)
	}

	wg.Wait()
}

func TestMap_EdgeCases(t *testing.T) {
	m := &Map[string, int]{}

	// Test with empty string key
	m.Store("", 42)
	value, ok := m.Load("")
	if !ok || value != 42 {
		t.Error("Empty string key failed")
	}

	// Test with zero value
	m.Store("zero", 0)
	value, ok = m.Load("zero")
	if !ok || value != 0 {
		t.Error("Zero value failed")
	}

	// Test LoadAndDelete on empty map
	_, loaded := m.LoadAndDelete("nonexistent")
	if loaded {
		t.Error("Expected LoadAndDelete to return loaded=false on empty map")
	}
}

func TestMap_ConcurrentModification(t *testing.T) {
	m := &Map[int, string]{}
	const numItems = 100

	// Pre-populate the map
	for i := 0; i < numItems; i++ {
		m.Store(i, "initial-"+strconv.Itoa(i))
	}

	// StartRenderLoop a goroutine that modifies the map while we iterate
	done := make(chan bool)
	go func() {
		for i := 0; i < numItems; i++ {
			m.Store(i, "modified-"+strconv.Itoa(i))
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()

	// Iterate over the map while it's being modified
	visited := 0
	m.Range(func(key int, value string) bool {
		visited++
		// Don't block the iteration
		return true
	})

	<-done

	// The exact count may vary due to concurrent modification
	if visited == 0 {
		t.Error("Expected to visit some items during concurrent modification")
	}
}

func BenchmarkMap_Store(b *testing.B) {
	m := &Map[int, string]{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Store(i, "value-"+strconv.Itoa(i))
	}
}

func BenchmarkMap_Load(b *testing.B) {
	m := &Map[int, string]{}
	// Pre-populate
	for i := 0; i < 1000; i++ {
		m.Store(i, "value-"+strconv.Itoa(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Load(i % 1000)
	}
}

func BenchmarkMap_LoadOrStore(b *testing.B) {
	m := &Map[int, string]{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.LoadOrStore(i%100, "value-"+strconv.Itoa(i))
	}
}
