"""Run suites after recording the snapshot's known fresh-install ordering failure.

The prerequisite workaround changes only newly created disposable databases.
It must not be mistaken for a successful unmodified install. No database is dropped.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--pg-bin', type=Path, required=True)
    parser.add_argument('--port', type=int, required=True)
    args = parser.parse_args()
    if args.port in (5432, 5435) or not 1024 <= args.port <= 65535:
        parser.error('A dedicated scratch PostgreSQL port is required.')
    env = os.environ.copy()
    for key in list(env):
        if any(word in key.upper() for word in ('SECRET', 'TOKEN', 'DATABASE_URL', 'WEBHOOK', 'SMTP', 'AWS_', 'AZURE_', 'BIGCOMMERCE', 'SHOPIFY')):
            env.pop(key, None)
    env.update(ENV='test', JWT_SECRET='local-audit-synthetic-secret-20260916-not-for-deployment',
               ERP_DISABLE_EXTERNAL_SIDE_EFFECTS='0')
    records = []
    def run(name, cmd):
        start = time.monotonic()
        with (args.evidence/(name+'.log')).open('w', encoding='utf-8') as log:
            try:
                result = subprocess.run([str(v) for v in cmd], cwd=args.source, env=env,
                                        stdout=log, stderr=subprocess.STDOUT, timeout=2400).returncode
            except (subprocess.TimeoutExpired, OSError) as error:
                log.write(str(error)); result = -1
        record = dict(id=name, command=[str(v) for v in cmd], exit_code=result,
                      seconds=round(time.monotonic()-start, 3), log=name+'.log')
        records.append(record)
        (args.evidence/'suite-command-results.json').write_text(json.dumps(records, indent=2), encoding='utf-8')
        print(json.dumps(record), flush=True)
        return result
    for i, seed in enumerate(('off', '4802', '4803'), 1):
        name = f'erp_audit_suite{i}'
        env.update(DATABASE_URL=f'postgres://erp_audit@127.0.0.1:{args.port}/{name}?sslmode=disable')
        env['TEST_DATABASE_URL'] = env['DATABASE_URL']
        prefix = f'suite-{i}'
        if run(prefix+'-create', [args.pg_bin/'createdb.exe', '-h', '127.0.0.1', '-p', args.port, '-U', 'erp_audit', name]):
            continue
        binary = args.evidence/'erp-audit.exe'
        result = run(prefix+'-fresh-migrate', [binary, '-migrate'])
        if result:
            log = (args.evidence/(prefix+'-fresh-migrate.log')).read_text(encoding='utf-8')
            if 'relation "tenant_default.audit_checkpoints" does not exist' not in log:
                continue
            if run(prefix+'-fixture-prerequisite', [args.pg_bin/'psql.exe', '-X', '-h', '127.0.0.1',
                   '-p', args.port, '-U', 'erp_audit', '-d', name, '-v', 'ON_ERROR_STOP=1',
                   '-f', args.source/'db/migrations_stage47_7_audit_evidence.sql']):
                continue
            if run(prefix+'-fixture-migrate', [binary, '-migrate']):
                continue
        run(prefix+'-fixture-replay', [binary, '-migrate'])
        command = ['go', 'test', './...', '-json', '-p', '1', '-count=1', '-shuffle='+seed, '-timeout=15m']
        if i == 1:
            command += ['-coverprofile='+str(args.evidence/'coverage.out')]
        run(prefix+'-tests', command)


if __name__ == '__main__':
    main()
