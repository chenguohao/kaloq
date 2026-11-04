#!/usr/bin/env python3
"""Run a command with the project's locally installed, pinned toolchain."""
import json
import os
from pathlib import Path
import platform
import sys


def main():
    if len(sys.argv) < 2:
        raise SystemExit('Usage: python3 scripts/with-toolchain.py <command> [args...]')
    root = Path(__file__).resolve().parents[1]
    lock = json.loads((root / 'toolchain.json').read_text())
    current_platform = f'{sys.platform}-{platform.machine()}'
    if current_platform != lock['platform']:
        raise SystemExit(f'Toolchain platform is {lock["platform"]}, current is {current_platform}')
    base = Path.home() / '.local/share/kaloq/toolchains'
    bins = [base / lock[name]['version'] / 'bin' for name in ('node', 'go')]
    for name, directory in zip(('node', 'go'), bins):
        if not os.access(directory / name, os.X_OK):
            raise SystemExit(f'Missing {name} toolchain. See docs/day-01/environment-and-checks.md')
    env = os.environ.copy()
    env.pop('GOROOT', None)
    env['PATH'] = os.pathsep.join([*(str(directory) for directory in bins), env.get('PATH', '')])
    env['GOTOOLCHAIN'] = 'local'
    os.execvpe(sys.argv[1], sys.argv[1:], env)


if __name__ == '__main__':
    main()
