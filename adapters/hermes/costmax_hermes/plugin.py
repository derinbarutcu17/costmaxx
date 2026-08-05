"""Hermes plugin integration for CostMax."""
import subprocess
import json
from typing import Any, Optional


class CostMaxHermesPlugin:
    """Integrates CostMax with the Hermes context engine."""

    def __init__(self, binary: str = "costmaxx"):
        self.binary = binary

    def get_state(self) -> dict[str, Any]:
        result = subprocess.run(
            [self.binary, "state"],
            capture_output=True, text=True, timeout=10,
        )
        return {"raw": result.stdout}

    def select_context(self, context: list[dict]) -> list[dict]:
        """Select the most relevant context items."""
        total_tokens = sum(
            c.get("tokens", 0) for c in context
        )
        budget = 4000
        if total_tokens <= budget:
            return context
        return context[:1]
