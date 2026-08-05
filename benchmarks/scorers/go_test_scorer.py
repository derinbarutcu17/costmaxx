"""CostMax benchmark scorer - runs tests and checks pass rate."""
import subprocess
import json
import sys


def score(task_id: str, repo_path: str) -> dict:
    result = subprocess.run(
        ["go", "test", "./..."],
        cwd=repo_path,
        capture_output=True,
        text=True,
        timeout=120,
    )
    passed = result.returncode == 0
    return {
        "task_id": task_id,
        "completed": passed,
        "exit_code": result.returncode,
        "stdout": result.stdout,
        "stderr": result.stderr,
    }


if __name__ == "__main__":
    task_id = sys.argv[1]
    repo_path = sys.argv[2]
    result = score(task_id, repo_path)
    print(json.dumps(result, indent=2))
