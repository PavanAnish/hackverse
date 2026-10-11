sed -i '' -e 's|for _, rd := range postInspect.RepoDigests {|postImg, _, _ := cli.ImageInspectWithRaw(ctx, postInspect.Image)\
	for _, rd := range postImg.RepoDigests {|' engines/executor/internal/engine/executor_integration_test.go
sed -i '' -e 's|postInspect.RepoDigests, authoritativeDigest|postImg.RepoDigests, authoritativeDigest|' engines/executor/internal/engine/executor_integration_test.go
