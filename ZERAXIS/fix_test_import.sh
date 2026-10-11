sed -i '' -e 's|"github.com/pavanvanish/zeraxis/engines/correlation/repository"||' engines/executor/internal/engine/executor_integration_test.go
sed -i '' -e 's|repo := repository.NewRepository(db)|// Simulated cross-service approval|' engines/executor/internal/engine/executor_integration_test.go
sed -i '' -e 's|execID, err := repo.ApproveRecommendation(ctx, recID, "user-admin")|execID, err := ApproveRecommendation(ctx, db, recID, "user-admin")|' engines/executor/internal/engine/executor_integration_test.go
