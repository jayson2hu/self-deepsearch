#!/usr/bin/env python3
"""Stop only the explicitly marked processes/container from one kept acceptance run."""
import argparse
import json
import os
import re
import signal
import subprocess
import time
from pathlib import Path


def stop(directory):
    access = json.loads((directory / 'access.json').read_text())
    run_id = access['run_id']
    if not re.fullmatch(r'selfdeepsearch-collector-e2e-[0-9a-f]{10}', run_id) or access['container'] != run_id:
        raise ValueError('unrecognized acceptance run')
    # Inspect every target before stopping any process; protects against PID reuse.
    live = []
    for pid in access['pids']:
        if not isinstance(pid, int) or pid <= 1:
            raise ValueError('invalid service PID')
        environment = Path(f'/proc/{pid}/environ')
        if not environment.exists():
            continue
        if f'COLLECTOR_E2E_RUN_ID={run_id}'.encode() not in environment.read_bytes().split(b'\0'):
            raise ValueError('PID no longer belongs to this acceptance run')
        live.append(pid)
    result = subprocess.run(['docker', 'inspect', '--format',
                             '{{index .Config.Labels "selfdeepsearch.collector-acceptance"}}', run_id],
                            capture_output=True, text=True, check=False)
    if result.returncode and 'No such object' not in result.stderr:
        raise ValueError('could not verify acceptance container')
    container_exists = result.returncode == 0
    if container_exists and result.stdout.strip() != run_id:
        raise ValueError('container no longer belongs to this acceptance run')
    for pid in live:
        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
    for _ in range(40):
        if all(not Path(f'/proc/{pid}').exists() for pid in live):
            break
        time.sleep(.25)
    for pid in live:
        environment = Path(f'/proc/{pid}/environ')
        if environment.exists() and f'COLLECTOR_E2E_RUN_ID={run_id}'.encode() in environment.read_bytes().split(b'\0'):
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
    if container_exists:
        subprocess.run(['docker', 'rm', '-f', run_id], check=True, capture_output=True)
    access['stopped_at'] = time.time()
    access.pop('password', None)
    (directory / 'access.json').write_text(json.dumps(access, indent=2) + '\n')
    print('Acceptance services stopped; evidence retained.')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path)
    stop(parser.parse_args().directory)
