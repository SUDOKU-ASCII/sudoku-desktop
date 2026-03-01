#!/usr/bin/env python3

import base64
import sys
from pathlib import Path


def main() -> int:
    path = Path(sys.argv[1] if len(sys.argv) > 1 else "cosign.key")
    data = path.read_bytes()
    sys.stdout.write(base64.b64encode(data).decode("utf-8"))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

