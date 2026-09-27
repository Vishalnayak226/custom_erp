"""Repeatable local ERP checks; requires an already isolated PostgreSQL cluster.

No package installation, production access, source changes, or database deletion.
The caller supplies a frozen source copy, empty evidence directory and scratch port.
Results contain commands and exit codes; logs may contain synthetic fixture data.
"""
import argparse
import datetime
import json
import os
from pathlib import Path
import subprocess
import time


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', required=True, type=Path)
    parser.add_argument('--evidence', required=True, type=Path)
    parser.add_argument('--pg-bin', required=True, type=Path)
    parser.add_argument('--port', required=True, type=int)
    args = parser.parse_args()
    if args.port in (5432, 5435) or not 1024 <= args.port <= 65535:
        parser.error('Use a dedicated scratch PostgreSQL port, never shared 5432/5435.')
    source = args.source.resolve()
    evidence = args.evidence.resolve()
    evidence.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    for key in list(env):
        if any(word in key.upper() for word in ('SECRET', 'TOKEN', 'DATABASE_URL', 'WEBHOOK', 'SMTP', 'AWS_', 'AZURE_', 'BIGCOMMERCE', 'SHOPIFY')):
            env.pop(key, None)
    env.update(ENV='test', JWT_SECRET='local-audit-synthetic-secret-20260916-not-for-deployment',
               ERP_DISABLE_EXTERNAL_SIDE_EFFECTS='0')
    records = []

    def run(name, command, timeout=1200, extra=None):
        started = datetime.datetime.now(datetime.timezone.utc).isoformat()
        before = time.monotonic()
        code = None
        with (evidence / (name + '.log')).open('w', encoding='utf-8') as log:
            try:
                code = subprocess.run([str(v) for v in command], cwd=source,
                                      env=env | (extra or {}), stdout=log,
                                      stderr=subprocess.STDOUT, timeout=timeout).returncode
            except (OSError, subprocess.TimeoutExpired) as error:
                log.write('\nHARNESS: ' + str(error) + '\n')
                code = -1
        record = dict(id=name, command=[str(v) for v in command], started_utc=started,
                      seconds=round(time.monotonic()-before, 3), exit_code=code,
                      log=name+'.log')
        records.append(record)
        (evidence / 'command-results.json').write_text(json.dumps(records, indent=2), encoding='utf-8')
        print(json.dumps(record), flush=True)
        return code

    pg = args.pg_bin
    def database(name):
        url = f'postgres://erp_audit@127.0.0.1:{args.port}/{name}?sslmode=disable'
        env.update(DATABASE_URL=url, TEST_DATABASE_URL=url)
        return run('create-'+name, [pg/'createdb.exe', '-h', '127.0.0.1', '-p', args.port,
                                  '-U', 'erp_audit', name]) == 0

    run('toolchain', ['go', 'version'])
    run('go-env', ['go', 'env', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'GOVERSION'])
    run('postgres-version', [pg/'psql.exe', '--version'])
    binary = evidence / 'erp-audit.exe'
    if run('build-server', ['go', 'build', '-trimpath', '-o', binary, './cmd/server']):
        return
    for iteration, seed in enumerate(('off', '4802', '4803'), start=1):
        prefix = f'iteration-{iteration}'
        if not database(f'erp_audit_pass{iteration}'):
            continue
        migrated = run(prefix+'-migrate', [binary, '-migrate']) == 0
        run(prefix+'-migrate-replay', [binary, '-migrate'])
        run(prefix+'-migrate-status', [binary, '-migrate-status'])
        for name, command in (
            ('modules', ['go', 'mod', 'verify']),
            ('build', ['go', 'build', './...']),
            ('vet', ['go', 'vet', './...']),
            ('doclint', ['go', 'run', './cmd/doclint', '-strict']),
            ('surface', ['go', 'run', './cmd/surfacescan', '-check']),
            ('bypass', ['go', 'run', './cmd/surfacescan', '-bypass']),
            ('browser-harness', ['node', '--test', 'docs/guides/capture-screenshots.test.cjs']),
        ):
            run(prefix+'-'+name, command)
        for js in ('public/app.js', 'public/db.js', 'public/qz-print.js', 'public/components/erp-typeahead.js'):
            run(prefix+'-syntax-'+Path(js).stem, ['node', '--check', js])
        if migrated:
            command = ['go', 'test', './...', '-json', '-p', '1', '-count=1',
                       f'-shuffle={seed}', '-timeout=15m']
            if iteration == 1:
                command += ['-coverprofile='+str(evidence/'coverage.out')]
            run(prefix+'-tests', command, timeout=2400)
        run(prefix+'-race', ['go', 'test', './db', '-race', '-count=1'])
        vuln = Path.home()/'go/bin/govulncheck.exe'
        if vuln.exists():
            run(prefix+'-vulnerabilities', [vuln, './...'])
    if database('erp_audit_ci'):
        # Mirror the workflow's filename ordering, stopping at the first error.
        for migration in sorted((source/'db').glob('*.sql')):
            if run('ci-migration-'+migration.stem, [pg/'psql.exe', '-X', '-h', '127.0.0.1',
                    '-p', args.port, '-U', 'erp_audit', '-d', 'erp_audit_ci',
                    '-v', 'ON_ERROR_STOP=1', '-f', migration]):
                break


if __name__ == '__main__':
    main()
