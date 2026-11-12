"""Bounded local HTTP checks and timing samples; no external traffic.

Requires the explicitly seeded 127.0.0.1:8178 audit server. Percentiles use
nearest rank; timings include local connection overhead and are not production SLO proof.
"""
import concurrent.futures
import gzip
import hashlib
import http.client
import json
import math
import argparse
from pathlib import Path
import time


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--evidence', type=Path, required=True)
    a = p.parse_args()
    token = (a.evidence/'ui-token.txt').read_text(encoding='utf-8-sig').strip()
    results = []
    def request(path, authenticated=True, method='GET', headers=None):
        conn = http.client.HTTPConnection('127.0.0.1', 8178, timeout=12)
        h = {'Accept-Encoding':'gzip'}
        if authenticated:
            h['Authorization'] = 'Bearer '+token
        h.update(headers or {})
        before = time.perf_counter()
        try:
            conn.request(method, path, headers=h)
            response = conn.getresponse(); data = response.read()
            decoded = gzip.decompress(data) if response.getheader('Content-Encoding') == 'gzip' else data
            record = dict(path=path, method=method, status=response.status,
                          ms=round((time.perf_counter()-before)*1000,3), wire_bytes=len(data),
                          decoded_bytes=len(decoded), body_sha256=hashlib.sha256(decoded).hexdigest(), headers=dict(response.getheaders()))
            try:
                body = json.loads(decoded)
                record['row_count'] = len(body) if isinstance(body,list) else len(body.get('data',[])) if isinstance(body,dict) and isinstance(body.get('data'),list) else None
                record['keys'] = sorted(body.keys()) if isinstance(body,dict) else None
            except (ValueError, UnicodeError):
                pass
            return record
        except Exception as error:
            return dict(path=path,status=0,error=str(error),ms=round((time.perf_counter()-before)*1000,3))
        finally:
            conn.close()
    checks = [
        ('anonymous-protected','/api/v1/doc/Item',False,'GET',{},(401,)),
        ('invalid-token','/api/v1/doc/Item',False,'GET',{'Authorization':'Bearer invalid'},(401,)),
        ('token-overrides-tenant-header','/api/v1/doc/Item',True,'GET',{'X-Tenant-ID':'audit-nonexistent'},(200,)),
        ('forged-resolved-role','/api/v1/admin/modules',False,'GET',{'Resolved-Role':'Super Admin','Resolved-Tenant-ID':'default'},(401,)),
        ('static-directory','/components/',False,'GET',{},(404,)),
        ('traversal','/%2e%2e/go.mod',False,'GET',{},(400,404)),
        ('static-write-method','/app.js',False,'PUT',{},(405,)),
        ('unknown-api','/api/v1/does-not-exist',True,'GET',{},(404,)),
    ]
    for iteration in range(1,4):
        for name,url,auth,method,headers,expected in checks:
            result=request(url,auth,method,headers);result.update(round=iteration,case=name,expected=list(expected),passed=result['status'] in expected);results.append(result)
            if name == 'token-overrides-tenant-header':
                baseline = request(url)
                result['same_as_token_tenant'] = result.get('body_sha256') == baseline.get('body_sha256')
                result['passed'] = result['passed'] and result['same_as_token_tenant']
        for asset in ('/','/app.js','/db.js','/styles.css','/components/erp-typeahead.js'):
            result=request(asset,False);result.update(round=iteration,case='asset');results.append(result)
        result=request('/api/v1/doc/Item?limit=1000000');result.update(round=iteration,case='oversized-page-bound');results.append(result)
        for concurrency in (1,4,8):
            with concurrent.futures.ThreadPoolExecutor(max_workers=concurrency) as pool:
                samples=list(pool.map(lambda _:request('/api/v1/doc/Item?limit=50'),range(24)))
            times=sorted(x['ms'] for x in samples)
            percent=lambda n:times[math.ceil(n*len(times))-1]
            result=dict(round=iteration,case='load',concurrency=concurrency,requests=len(samples),
                        non_200=sum(x['status']!=200 for x in samples),p50_ms=percent(.5),p95_ms=percent(.95),p99_ms=percent(.99),samples=samples)
            results.append(result)
            print(json.dumps({k:v for k,v in result.items() if k!='samples'}),flush=True)
        (a.evidence/'http-results.json').write_text(json.dumps(results,indent=2),encoding='utf-8')
        if iteration<3:
            # A round uses 72 list requests; the actual search budget is 100/minute.
            # Keep bounded load rounds in separate windows. Preserve any 429 as evidence.
            time.sleep(30)
            time.sleep(31)


if __name__ == '__main__':
    main()
