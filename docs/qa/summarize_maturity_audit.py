"""Summarize the recorded September audit without turning missing checks into passes."""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import re


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--evidence',type=Path,required=True)
    p.add_argument('--output',type=Path,required=True)
    args=p.parse_args(); e=args.evidence
    def read(name):return (e/name).read_text(encoding='utf-8-sig')
    def data(name):return json.loads(read(name))
    source=data('source-manifest.json');root=Path(source['snapshot'])
    changed=[]
    for entry in source['files']:
        path=root/entry['path']
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest()!=entry['sha256']:
            changed.append(entry['path'])
    suites=[]
    for iteration in range(1,4):
        counts=Counter();failed=[];skipped=[];packages={};coverage={}
        for line in read(f'suite-{iteration}-tests.log').splitlines():
            try:event=json.loads(line)
            except ValueError:continue
            action=event.get('Action');name=event.get('Test');package=event.get('Package')
            if name and action in ('pass','fail','skip'):counts[action]+=1
            if name and action=='fail':failed.append({'package':package,'test':name})
            if name and action=='skip':skipped.append({'package':package,'test':name})
            if not name and action in ('pass','fail','skip'):packages[package]=action
            if action=='output':
                match=re.search(r'coverage: ([\d.]+)% of statements',event.get('Output',''))
                if match:coverage[package]=float(match[1])
        suites.append(dict(iteration=iteration,counts=dict(counts),failures=failed,skips=skipped,packages=packages,statement_coverage=coverage))
    browser=data('browser-results.json');http=data('http-results.json');packages=data('package-probe-results.json')
    vulnerability_ids=lambda name:re.findall(r'Vulnerability #\d+: (GO-[\d-]+)',read(name))
    summary={
        'schema_version':1,'audit_date':'2026-09-16','scope':'Frozen local source; synthetic fixtures; no production deployment or certification',
        'source':{'head':source['head'],'captured_utc':source['captured_utc'],'files':len(source['files']),
                  'manifest_sha256':hashlib.sha256((e/'source-manifest.json').read_bytes()).hexdigest(),
                  'snapshot_unchanged':not changed,'changed_files':changed},
        'environment':{'os':'Windows amd64','go':'1.26.5','compatibility_build_go':'1.22.12','postgres':'16.3',
                       'postgres_port':5446,'http_port':8178,'browser':browser['browser'],'cgo':False},
        'suite_prerequisite':'Fresh migration fails; prerequisite audit-evidence SQL explicitly applied in disposable fixtures before continuing unchanged migrations.',
        'suites':suites,'top_level_test_declarations':len(data('test-inventory.json')),
        'go_fuzz_declarations':0,'go_benchmark_declarations':0,
        'overall_statement_coverage_percent':float(re.search(r'total:.*?([\d.]+)%',read('coverage-summary.log')).group(1)),
        'browser':{'contexts':sorted(set(r['context'] for r in browser['results'])),'observations':len(browser['results']),
                   'unique_views':len(browser['views']),'document_overflows':sum(r['documentWidth']>r['viewport'] for r in browser['results']),
                   'state_dependent_hook_log_failures':sum(r['view']=='extension-hook-log' and bool(r['errors']) for r in browser['results'])},
        'ui_tasks':data('ui-task-results.json'),'cross_role_xss':data('xss-cross-role-view.json'),
        'http_checks':[{'round':r['round'],'case':r['case'],'status':r['status'],'passed':r['passed']} for r in http if 'passed' in r],
        'load_samples':[{k:v for k,v in r.items() if k!='samples'} for r in http if r['case']=='load'],
        'assets':[{'round':r['round'],'path':r['path'],'wire_bytes':r['wire_bytes'],'decoded_bytes':r['decoded_bytes']} for r in http if r['case']=='asset'],
        'oversized_page_rows':[r['row_count'] for r in http if r['case']=='oversized-page-bound'],
        'linux_builds':data('linux-build-results.json'),'memory_samples':data('process-memory-after-browse.json'),
        'restore_drills':data('restore-results.json'),
        'package_checks':{'normal_checks':sum('matches' in r for r in packages),'normal_passes':sum(r.get('matches',False) for r in packages),
                          'fault_cases':[r for r in packages if r.get('case')=='disable-write-failure']},
        'quota_failure_checks':data('tenant-limit-probe.json'),
        'repository_vulnerability_ids':vulnerability_ids('iteration-1-vulnerabilities.log'),
        'server_vulnerability_ids':vulnerability_ids('server-vulnerabilities.log'),
        'normalized_documentation_safety':[{'iteration':i,'checks':read(f'docs-safety-normalized-{i}.log').count('PASS read-only'),
                                          'passed':'Documentation safety integration checks passed.' in read(f'docs-safety-normalized-{i}.log')} for i in range(1,4)],
        'confirmed_defect_ids':[f'AUD-{i:02d}' for i in range(1,10)],
        'unexecuted':['race detector without C compiler','Unix archive permissions','local process restart rejected by automatic approval review',
                      'production load','multi-day soak','real providers and physical devices','Firefox/WebKit','qualified legal/accounting/payroll and accessibility acceptance'],
        'interpretation_notes':[
            'Test counts include parent tests and subtests, not unique assertions.',
            'Three full suites have test failures; do not report them as passed.',
            'Initial HTTP overload results are separate from successful-request latency samples.',
            'The original UI task raw-body string comparison mishandled JSON HTML escaping; parsed cross-role verification confirms Unicode persistence.',
            'Git-less documentation inventory false positives are excluded; use the three Git-context lint logs.',
            'Documentation safety passes only after normalizing generated files in a separate fixture; baseline freshness still fails.',
            'Normal package convergence is not complete standalone-product acceptance.',
        ],
        'evidence_files':[{ 'file':f.name,'bytes':f.stat().st_size,'sha256':hashlib.sha256(f.read_bytes()).hexdigest()}
                          for f in sorted(e.iterdir()) if f.is_file() and f.suffix in ('.json','.log','.png','.out') and 'token' not in f.name],
    }
    args.output.parent.mkdir(parents=True,exist_ok=True)
    args.output.write_text(json.dumps(summary,indent=2,ensure_ascii=True)+'\n',encoding='utf-8')
    print(json.dumps({'snapshot_unchanged':not changed,'suite_counts':[s['counts'] for s in suites],
                      'browser_observations':summary['browser']['observations'],'normalized_safety':summary['normalized_documentation_safety']}))


if __name__=='__main__':main()
