package v8

import (
	"github.com/yaoapp/gou/process"
	"github.com/yaoapp/kun/exception"
)

func init() {
	process.Register("scripts", processScripts)
	process.Register("studio", processStudio)
}

// processScripts
func processScripts(process *process.Process) interface{} {

	script, err := Select(process.ID)
	if err != nil {
		exception.New("scripts.%s.%s not loaded: %s", 404, process.ID, process.Method, err).Throw()
		return nil
	}

	return script.Exec(process)
}

// processScripts scripts.ID.Method
func processStudio(process *process.Process) interface{} {

	script, err := SelectRoot(process.ID)
	if err != nil {
		exception.New("studio.%s.%s not loaded: %s", 404, process.ID, process.Method, err).Throw()
		return nil
	}
	return script.Exec(process)

}
