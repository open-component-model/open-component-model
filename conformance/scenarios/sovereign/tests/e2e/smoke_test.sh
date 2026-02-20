#!/bin/bash
# Smoke test for sovereign scenario deployment

set -euo pipefail

NAMESPACE="sovereign-product"
TIMEOUT="300s"

echo "🧪 Running sovereign scenario smoke tests"

# Test 1: Check pods are running
echo "Checking pod status..."
kubectl -n $NAMESPACE get pods
kubectl -n $NAMESPACE wait --for=condition=Ready pod --all --timeout=$TIMEOUT

# Test 2: Check services are available  
echo "Checking service status..."
kubectl -n $NAMESPACE get svc

# Test 3: Test application endpoints
echo "Testing application connectivity..."
kubectl -n $NAMESPACE port-forward svc/notes 8080:80 &
PORT_FORWARD_PID=$!
sleep 5

# Health check
if curl -f http://localhost:8080/healthz; then
    echo "✅ Health check passed"
else
    echo "❌ Health check failed"
    exit 1
fi

# Readiness check
if curl -f http://localhost:8080/readyz; then
    echo "✅ Readiness check passed"
else
    echo "❌ Readiness check failed"
    exit 1
fi

# API functionality check
if curl -f http://localhost:8080/notes; then
    echo "✅ Notes API responding"
else
    echo "❌ Notes API failed"
    exit 1
fi

# Cleanup
kill $PORT_FORWARD_PID || true

echo "🎉 All smoke tests passed!"