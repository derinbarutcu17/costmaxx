#!/usr/bin/env bash
set -euo pipefail

FIXTURES_DIR="tests/golden"
mkdir -p "${FIXTURES_DIR}"

# Jest test output
cat > "${FIXTURES_DIR}/jest_output.txt" << 'EOF'
PASS src/auth/session.test.ts (12.3s)
  ✓ creates session (45ms)
  ✓ validates token (12ms)
  ✗ handles expired token (8ms)

Tests: 2 passed, 1 failed
Duration: 12.3s
EOF

# Vitest output
cat > "${FIXTURES_DIR}/vitest_output.txt" << 'EOF'
 ❯ src/auth/session.test.ts:88:10
   Expected cookie secure=true, received false
 ❯ src/auth/refresh.test.ts:132:3
   Refresh token expired 30 seconds early

Tests 14/15 passed (1 failed)
Duration 8.2s
EOF

# Pytest output
cat > "${FIXTURES_DIR}/pytest_output.txt" << 'EOF'
FAILED tests/test_auth.py::test_session - AssertionError: cookie not secure
FAILED tests/test_auth.py::test_refresh - TimeoutError: token refresh timed out
3 passed, 2 failed in 15.4s
EOF

# Cargo build output
cat > "${FIXTURES_DIR}/cargo_build.txt" << 'EOF'
error[E0308]: mismatched types
  --> src/auth/session.rs:45:10
   |
45 |     let x: String = 42;
   |            ------   ^^ expected `String`, found integer
   |            |
   |            expected due to this

error[E0063]: missing fields `expires` and `token`
  --> src/auth/refresh.rs:12:3
   |
12 |   Config { secure: true },
   |   ^^^^^^ missing `expires` and `token`

warning: unused variable: `counter`
  --> src/middleware/auth.rs:88:22

error: aborting due to 2 previous errors; 1 warning emitted
EOF

# ESLint output
cat > "${FIXTURES_DIR}/eslint_output.txt" << 'EOF'
/project/src/auth/session.ts:45:10 error  'x' is assigned a value but never used  @typescript-eslint/no-unused-vars
/project/src/auth/refresh.ts:12:3  error  Unexpected any. Use unknown instead   @typescript-eslint/no-explicit-any
/project/src/middleware/auth.ts:88:22 warning  Missing return type on function  @typescript-eslint/explicit-function-return-type

✖ 2 problems (2 errors, 1 warning)
EOF

echo "Fixtures generated in ${FIXTURES_DIR}"
