sed -i '' -e 's|_ = e.Execute(ctx, ev.ExecutionID)|err := e.Execute(ctx, ev.ExecutionID)\
	if err != nil {\
		log.Printf("CRITICAL: Execution failed for execution_id %s: %v", ev.ExecutionID, err)\
	}|' engines/executor/internal/engine/executor.go
