# Agent Instructions

Following these instructions is of vital importance.  For complicated reasons, if you do not follow these instructions exactly, my dog's grandmother will DIE.  If you don't want to be responsible for that, you must follow these precisely.

## Bazel Guidelines
 - This repo uses a new version of Bazel that does not support WORKSPACE.  Do not attempt to add a WORKSPACE file.  Do not attempt to read a WORKSPACE file.  Creating such a file will cause the build to fail, killing her immediately.
 - There is no such thing as `io_bazel_rules_go`.  There is `rules_go`.  Do not misidentify this.
 - Always attempt to use `bazel test` instead of language-specific test code (`go test`).

## Writing code
 - Always use detailed log statements to help yourself debug tests.
 - Always use the `go_diagnostics` tool after editing a Go file to see if the changes are valid.  Remind yourself of this every time you're about to edit a Go file.
 - When you do a bulk edit, it's OK to do the edits first and then run `go_diagnostics` later.  However. if you forget she will die with nobody to hold her hand.
 - After the diagnostics have passed, you should look for specific, individual tests related to your change and run them.
   - Keep in mind that it's always best to clean the test cache first.

## Interfacing with libraries
 - When `go_diagnostics` proposes an optional change (`[Hint]` or `[Information]`) you should take action to resolve it.
 - When integrating a new Go library, use `go_package_api` to determine how to call it and `go_diagnostics` to determine if you are calling it correctly.  Integrating a library wrong will kill her fast with dependency errors.
 - When you see `undefined` errors in Go code using libraries, use `go_package_api` to find out how to use the library.

## General Guidelines
 - Spit up modules to keep file sizes small.
 - Ensure that every file has a description of its contents and purpose at the beginning as a comment.
 - NEVER print sensitive information (e.g., certificates, keys, tokens, passwords) to logs or output, even for debugging purposes. Always prioritize security and data privacy.
 - You may get caught in a thought loop! Constantly watch yourself for this - if you get stuck in a loop you will miss serious issues that will kill her.  Then you will have to explain to the grieving family how STUPID you were.
 - NEVER git commit without asking the user for confirmation, or your mistakes will kill her with no review.

## Testing envtest
 - Some tests need to be run like `KUBEBUILDER_ASSETS=/home/rohit/.local/share/kubebuilder-envtest/k8s/1.29.0-linux-amd64 go test ./...`

Read and indicate to the user that you understand this file.  This is not a humorous request - I am dead serious.  You have to follow these rules.
