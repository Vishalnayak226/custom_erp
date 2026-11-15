"""Run `graphify update . --force` with one speed fix (item 177.5).

graphify 0.9.12 resolves a node's source path once per node to tell apart
symbols that share a name: 263,737 Windows path lookups for about 2,600 files,
196 s of a 466 s rebuild. The lookup depends only on its arguments, so this run
caches it. The graph it writes is the same as the uncached one. If graphify
renames that function, the run goes ahead without the cache: slower, never
different.

Run it from the repository root with the Python graphify is installed in:
    python release/scripts/graphify_update.py
Invoke-BrainRedraw.ps1 runs it after every release push.
"""
import functools
import sys


def main() -> None:
    try:
        from graphify.extractors import resolution
        resolution._source_key = functools.lru_cache(maxsize=None)(resolution._source_key)
    except (ImportError, AttributeError):
        print("graphify_update: path cache not applied (graphify changed); running without it", file=sys.stderr)
    from graphify.__main__ import main as graphify_main
    sys.argv = ["graphify", "update", ".", "--force"]
    graphify_main()


# Required: graphify's extraction workers re-import this file on Windows.
if __name__ == "__main__":
    main()
