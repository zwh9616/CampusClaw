//go:build testhooks

package materials

// The setters below exist only in test builds. Compiling without the testhooks
// tag removes them entirely, which is what makes the fault injection
// unavailable in production rather than merely unused.

// SetKnowledgeInsertHook installs a fault for the knowledge insert step.
func SetKnowledgeInsertHook(hook func() error) {
	knowledgeInsertHook = hook
}

// SetFailFileDeletes makes compensation deletes fail.
func SetFailFileDeletes(fail bool) {
	failFileDeletes = fail
}

// SetUnknownCommit makes a successful commit report an unknown result.
func SetUnknownCommit(unknown bool) {
	unknownCommit = unknown
}

// ResetHooks clears every installed fault.
func ResetHooks() {
	knowledgeInsertHook = nil
	failFileDeletes = false
	unknownCommit = false
}
