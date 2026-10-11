sed -i '' -e '/targetDigest := "sha256:dummy_digest"/,/}/c\
	imgInspect, _, _ := cli.ImageInspectWithRaw(ctx, inspect.Image)\
	targetDigest := "sha256:dummy_digest"\
	if len(imgInspect.RepoDigests) > 0 {\
		targetDigest = imgInspect.RepoDigests[0]\
	}\
' engines/executor/internal/engine/executor_integration_test.go
