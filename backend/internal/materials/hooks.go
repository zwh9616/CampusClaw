package materials

// Fault-injection seams used by the integration suite to prove the rollback and
// compensation paths really run.
//
// In the default build there is no setter for any of these, so a production
// binary has no code path that can populate them and no fault API to trigger.
// The setters live in hooks_testhooks.go behind the `testhooks` build tag.

// knowledgeInsertHook makes the second statement of the upload transaction
// fail, exercising the rollback of the material insert.
var knowledgeInsertHook func() error

// failFileDeletes makes the compensation delete after a failed transaction
// fail, exercising the "report failure, log for manual review" path.
var failFileDeletes bool

// unknownCommit makes a committed transaction report an indeterminate result,
// exercising the "never blindly delete a possibly-committed file" path.
var unknownCommit bool
