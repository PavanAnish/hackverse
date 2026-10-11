sed -i '' -e 's|github.com/nats-io/nats.go|github.com/nats-io/nats.go\
\	"github.com/pavanvanish/zeraxis/engines/correlation/internal/repository"|' engines/executor/internal/engine/executor_integration_test.go

sed -i '' -e '/execID := uuid.New().String()/,/VALUES ($1, $2, '\''user-admin'\'', '\''APPROVED'\'', NOW())", execID, recID)/c\
	repo := repository.NewRepository(db)\
	execID, err := repo.ApproveRecommendation(ctx, recID, "user-admin")\
	if err != nil {\
		t.Fatalf("ApproveRecommendation failed: %v", err)\
	}\
' engines/executor/internal/engine/executor_integration_test.go
