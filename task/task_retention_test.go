package task

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTaskNextIDAndRetention(t *testing.T) {
	handlers := &Handlers{
		Exec: func(id int, args ...interface{}) (interface{}, error) {
			return "done:" + args[0].(string), nil
		},
	}

	opt := Option{
		Name:           "test_retention",
		WorkerNums:     2,
		JobQueueLength: 10,
		Timeout:        5,
	}

	tsk := New(handlers, opt)
	go tsk.Start()
	defer tsk.Stop()

	// 1. Test monotonic nextID
	id1, err := tsk.Add("job1")
	assert.NoError(t, err)
	id2, err := tsk.Add("job2")
	assert.NoError(t, err)
	id3, err := tsk.Add("job3")
	assert.NoError(t, err)

	assert.Equal(t, 1, id1)
	assert.Equal(t, 2, id2)
	assert.Equal(t, 3, id3)

	// Wait for execution
	time.Sleep(50 * time.Millisecond)

	// 2. Test completed job retention
	info1, err := tsk.Get(id1)
	assert.NoError(t, err)
	assert.Equal(t, "SUCCESS", info1["status"])
	assert.Equal(t, "done:job1", info1["response"])

	info2, err := tsk.Get(id2)
	assert.NoError(t, err)
	assert.Equal(t, "SUCCESS", info2["status"])
	assert.Equal(t, "done:job2", info2["response"])

	// 3. Test nextID continues monotonically after queue empty
	id4, err := tsk.Add("job4")
	assert.NoError(t, err)
	assert.Equal(t, 4, id4)
}

func TestTaskFailureStatus(t *testing.T) {
	handlers := &Handlers{
		Exec: func(id int, args ...interface{}) (interface{}, error) {
			return nil, assert.AnError
		},
	}

	opt := Option{
		Name:           "test_failure",
		WorkerNums:     1,
		JobQueueLength: 5,
		Timeout:        5,
	}

	tsk := New(handlers, opt)
	go tsk.Start()
	defer tsk.Stop()

	id, err := tsk.Add("fail_job")
	assert.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	info, err := tsk.Get(id)
	assert.NoError(t, err)
	assert.Equal(t, "FAILURE", info["status"])
}
