"""Exercise the real remote deployment script with local command doubles.

Only fresh TEMP children are used. No SSH, services, HTTP, alerts or real migrations.
The public upload step mirrors deploy.ps1 before invoking remote_deploy.sh.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--source', type=Path, required=True)
    p.add_argument('--evidence', type=Path, required=True)
    p.add_argument('--bash', type=Path, required=True)
    a = p.parse_args()
    root = Path(tempfile.mkdtemp(prefix='erp-audit-deploy-')).resolve()
    if root.parent != Path(tempfile.gettempdir()).resolve():
        raise RuntimeError('Fixture must be a direct TEMP child')
    results = []
    for iteration in range(1, 4):
        for case in ('healthy', 'unhealthy', 'restart-fails', 'migration-fails'):
            box = (root/f'{iteration}-{case}').resolve()
            if box.parent != root:
                raise RuntimeError('Fixture escaped scratch root')
            box.mkdir(); (box/'public').mkdir(); (box/'public.new').mkdir(); (box/'deploy').mkdir(); (box/'bin').mkdir()
            (box/'erp-server').write_text('OLD_BINARY')
            (box/'erp-server.new').write_text('NEW_BINARY')
            (box/'public/app.js').write_text('OLD_PUBLIC')
            # Stage 50/BLD-005: deploy.ps1 stages the new frontend to public.new
            # and remote_deploy.sh activates it alongside the binary swap - it no
            # longer overwrites the live public/ before this script's own
            # pre-deploy snapshot runs (that ordering bug was real: it made every
            # rollback "restore" a snapshot that was already the new release; see
            # the ledger/checklist entry this fix cites).
            (box/'public.new/app.js').write_text('NEW_PUBLIC')
            # Stage 50/BLD-006: a separate "what is actually running" marker.
            # Earlier revisions of this stub inferred liveness straight from
            # the erp-server FILE, which made every case where the file swap
            # succeeded but systemctl restart itself failed indistinguishable
            # from a real successful restart - exactly the false-success gap
            # remote_deploy.sh's commit-aware wait_healthy now closes. Only
            # the systemctl stub below (invoked via sudo, so restart-fails
            # correctly never reaches it) updates this file.
            (box/'.running_commit').write_text('old-commit')
            shutil.copyfile(a.source/'deploy/remote_deploy.sh', box/'deploy/remote_deploy.sh')
            scripts = {
                'sudo': 'if [ "$AUDIT_CASE" = "restart-fails" ]; then exit 1; fi\n"$@"\n',
                # Only reached when the sudo stub actually forwards its
                # argv (i.e. NOT restart-fails), matching sudo's own
                # exit-1-before-exec behavior for that case - a real
                # `systemctl restart` failure never reaches the service
                # manager either. Updates .running_commit to whatever build
                # is currently on disk, which is what makes a genuine
                # restart success/failure distinguishable below.
                'systemctl': (
                    'if [ "$1" = "restart" ]; then\n'
                    '  commit=old-commit\n'
                    '  if [ "$(cat "$REMOTE_DIR/erp-server")" = "NEW_BINARY" ]; then\n'
                    '    commit="$DEPLOY_COMMIT"\n'
                    '  fi\n'
                    '  printf "%s" "$commit" > "$REMOTE_DIR/.running_commit"\n'
                    'fi\n'
                    'exit 0\n'
                ),
                'sleep': 'exit 0\n',
                'journalctl': 'echo synthetic-journal\n',
                # Stage 50/BLD-006: remote_deploy.sh's wait_healthy now reads
                # git_commit out of the health response body (not just the
                # status code) to tell "the new build is actually serving"
                # apart from "something 200s". Keyed off .running_commit
                # (what a real restart actually activated), not the
                # erp-server file directly (what is merely staged on disk) -
                # those two only agree when systemctl restart truly
                # succeeded, which is the exact distinction this fix exists
                # to make.
                'curl': (
                    'running="$(cat "$REMOTE_DIR/.running_commit" 2>/dev/null || echo old-commit)"\n'
                    'status=200\n'
                    'if [ "$AUDIT_CASE" = "unhealthy" ] && [ "$running" = "$DEPLOY_COMMIT" ]; then\n'
                    '  status=503\n'
                    'fi\n'
                    'case "$*" in\n'
                    '  *"-o /dev/null"*) printf "%s" "$status" ;;\n'
                    '  *) printf "{\\"status\\":\\"ok\\",\\"git_commit\\":\\"%s\\"}" "$running" ;;\n'
                    'esac\n'
                ),
            }
            for name, text in scripts.items():
                (box/'bin'/name).write_text('#!/usr/bin/env bash\n'+text, newline='\n')
            (box/'deploy/migrate.sh').write_text('#!/usr/bin/env bash\nif [ "$AUDIT_CASE" = "migration-fails" ]; then exit 1; fi\nexit 0\n', newline='\n')
            env = os.environ.copy()
            env.pop('OPS_ALERT_WEBHOOK_URL', None)
            env.update(REMOTE_DIR=box.as_posix(), AUDIT_CASE=case, DEPLOY_COMMIT='synthetic-audit')
            # Git Bash receives its PATH in POSIX form from a literal script in this fixture.
            (box/'run.sh').write_text('REMOTE_DIR="$(cygpath -u "$REMOTE_DIR")"\nexport REMOTE_DIR\nexport PATH="$REMOTE_DIR/bin:$PATH"\nchmod +x "$REMOTE_DIR"/bin/*\nbash "$REMOTE_DIR/deploy/remote_deploy.sh"\n', newline='\n')
            result = subprocess.run([str(a.bash), str(box/'run.sh')], cwd=box, env=env,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=30)
            actual = {'binary':(box/'erp-server').read_text() if (box/'erp-server').exists() else 'absent',
                      'public':(box/'public/app.js').read_text()}
            expected = {'binary':'NEW_BINARY','public':'NEW_PUBLIC'} if case == 'healthy' else {'binary':'OLD_BINARY','public':'OLD_PUBLIC'}
            record = dict(iteration=iteration, case=case, exit_code=result.returncode,
                          expected=expected, actual=actual, release_consistent=actual==expected,
                          output=result.stdout, fixture=str(box))
            results.append(record)
            print(json.dumps({k:v for k,v in record.items() if k not in ('output','fixture')}), flush=True)
    (a.evidence/'deploy-mock-results.json').write_text(json.dumps(results,indent=2),encoding='utf-8')


if __name__ == '__main__':
    main()
