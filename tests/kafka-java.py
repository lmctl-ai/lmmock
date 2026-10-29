#!/usr/bin/env python3
"""Optional real lmclient/kafka-clients smoke test, no Docker.
Usage: python3 tests/kafka-java.py build/lmmock /path/to/lmclient-all.jar
Requires Java 21+. Does not build or alter the external lmclient checkout.
"""
import json
from pathlib import Path
import os
import subprocess
import sys
import tempfile
import time

binary, jar = map(lambda p: str(Path(p).resolve()), sys.argv[1:])
with tempfile.TemporaryDirectory(prefix='lmmock-kafka-') as directory:
    ready = Path(directory) / 'ready.json'
    server = subprocess.Popen([binary, 'proto', 'kafka', '--ready-file', str(ready)],
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        for _ in range(100):
            if ready.exists():
                break
            if server.poll() is not None:
                raise AssertionError(server.communicate())
            time.sleep(.05)
        info = json.loads(ready.read_text())
        address = info['address']
        env = {**os.environ, 'LMCLIENT_KAFKA_URL': address}
        def run(*args, expected):
            result = subprocess.run(['java', '-jar', jar, 'kafka', *args], env=env,
                                    capture_output=True, text=True, timeout=25)
            assert result.returncode == 0, (result.stdout, result.stderr)
            envelope = json.loads(result.stdout)
            assert envelope['ok'] and envelope['result'] == expected, envelope
            print('kafka ' + ' '.join(args) + ' -> ' + json.dumps(envelope, ensure_ascii=False))
        run('ping', expected={'connected': True})
        run('ping', '--url', address, expected={'connected': True})
        messages = ['first', 'héllo "Kafka"\nsecond', 'third']
        for message in messages:
            run('produce', '--topic', 'roundtrip', '--message', message, expected={'published': True})
        run('consume', '--topic', 'roundtrip', '--limit', '3', '--timeout', '5000', expected={'messages': messages})
        run('consume', '--topic', 'roundtrip', '--limit', '1', expected={'messages': messages[:1]})
        run('consume', '--topic', 'roundtrip', '--limit', '5', '--timeout', '1500', expected={'messages': messages})
        run('produce', '--topic', 'empty-value', '--message', '', expected={'published': True})
        run('consume', '--topic', 'empty-value', expected={'messages': ['']})
        print('PASS 10 real Java Kafka checks: ping, idempotent produce, UTF-8 order, replay, partial deadline, empty value')
    finally:
        server.terminate()
        try:
            server.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            server.kill()
            server.communicate()
            raise AssertionError('mock did not shut down within five seconds')
