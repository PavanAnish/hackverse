#!/bin/bash

# Give services a moment to be fully ready
echo "Waiting for services to be ready..."
sleep 2

# Helper function to test health
test_health() {
    local url=$1
    local name=$2
    
    echo -n "Testing $name health ($url)... "
    HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$url")
    
    if [ "$HTTP_STATUS" -eq 200 ]; then
        echo "✅ OK"
    else
        echo "❌ FAILED (HTTP $HTTP_STATUS)"
        exit 1
    fi
}

test_health "http://localhost:8080/health" "Gateway"
test_health "http://localhost:8080/api/auth/health" "Auth Service (via Gateway)"
test_health "http://localhost:8080/api/products/health" "Product Service (via Gateway)"
test_health "http://localhost:8080/api/payments/health" "Payment Service (via Gateway)"

echo "All health checks passed!"
