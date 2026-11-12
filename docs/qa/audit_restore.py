"""Three local dump/restore fidelity drills on a quiescent audit database.

Creates new databases only. Requires the dedicated cluster on 5446 and the named
synthetic source fixture. Table fingerprints test accidental differences, not adversarial integrity.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence',type=Path,required=True)
    parser.add_argument('--pg-bin',type=Path,required=True)
    args=parser.parse_args()
    env=os.environ.copy();env['PGCLIENTENCODING']='UTF8'
    connection=['-h','127.0.0.1','-p','5446','-U','erp_audit']
    def sql(database,query):
        return subprocess.check_output([str(args.pg_bin/'psql.exe'),'-X','-A','-t',*connection,'-d',database,'-v','ON_ERROR_STOP=1','-c',query],env=env,text=True,encoding='utf-8').strip()
    source='erp_audit_repro1'
    sql(source,"INSERT INTO tenant_default.documents (id,doctype,data,status,created_by) SELECT 'AUDIT-RESTORE-'||g,'Item',jsonb_build_object('name','Synthetic restore नमस्ते 中文 🧾 '||g,'sale_price',100.25),'Active','admin' FROM generate_series(1,5000) g ON CONFLICT DO NOTHING")
    tables=[line.split('|') for line in sql(source,"SELECT table_schema,table_name FROM information_schema.tables WHERE table_type='BASE TABLE' AND table_schema NOT IN ('pg_catalog','information_schema') ORDER BY table_schema,table_name").splitlines()]
    def identifier(value):return '"'+value.replace('"','""')+'"'
    def fingerprint(database):
        queries=[]
        for schema,table in tables:
            label=(schema+'.'+table).replace("'","''")
            queries.append(f"SELECT '{label}',count(*),md5(COALESCE(string_agg(row_to_json(t)::text,E'\\n' ORDER BY row_to_json(t)::text),'')) FROM {identifier(schema)}.{identifier(table)} t")
        raw=sql(database,';'.join(queries))
        return {'table_count':len(tables),'rows':sum(int(line.split('|')[1]) for line in raw.splitlines()),'sha256_of_table_fingerprints':hashlib.sha256(raw.encode()).hexdigest()}
    baseline=fingerprint(source);records=[]
    for iteration in range(1,4):
        dump=args.evidence/f'restore-drill-{iteration}.dump'
        start=time.perf_counter()
        subprocess.run([str(args.pg_bin/'pg_dump.exe'),*connection,'-d',source,'-Fc','--no-owner','--no-acl','-f',str(dump)],env=env,check=True)
        dump_seconds=time.perf_counter()-start
        target=f'erp_audit_restore{iteration}'
        subprocess.run([str(args.pg_bin/'createdb.exe'),*connection,target],env=env,check=True)
        start=time.perf_counter()
        with (args.evidence/f'restore-drill-{iteration}.log').open('w',encoding='utf-8') as log:
            subprocess.run([str(args.pg_bin/'pg_restore.exe'),*connection,'-d',target,'--no-owner','--no-acl','--exit-on-error',str(dump)],env=env,stdout=log,stderr=subprocess.STDOUT,check=True)
        restore_seconds=time.perf_counter()-start
        restored=fingerprint(target)
        result=dict(iteration=iteration,source=source,target=target,baseline=baseline,restored=restored,
                    matches=baseline==restored,dump_bytes=dump.stat().st_size,
                    dump_seconds=round(dump_seconds,3),restore_seconds=round(restore_seconds,3))
        records.append(result);print(json.dumps(result),flush=True)
        (args.evidence/'restore-results.json').write_text(json.dumps(records,indent=2),encoding='utf-8')


if __name__=='__main__':main()
