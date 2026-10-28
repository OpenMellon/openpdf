#!/usr/bin/env python3
"""Fail when a benchmark got significantly slower.

Usage: bench_compare.py <benchstat -format csv output> [threshold-percent]

benchstat prints "~" in the "vs base" column unless the difference is statistically significant (p < 0.05), so only
significant slowdowns larger than the threshold (default 25%, to allow for shared-runner noise) fail the check.
Only the first table (time per operation) is checked.
"""
import csv
import sys


def main() -> int:
    path = sys.argv[1]
    threshold = float(sys.argv[2]) if len(sys.argv) > 2 else 25.0
    in_table = False
    seen_header = False
    rows = 0
    regressions = []
    with open(path, newline="") as f:
        for row in csv.reader(f):
            if len(row) >= 6 and row[1] == "sec/op" and not seen_header:
                in_table = seen_header = True
                continue
            if in_table and len(row) >= 2 and row[0] == "" and row[1] != "sec/op":
                break  # the header of the next table (csv.reader drops the blank line between tables)
            if in_table and len(row) >= 2 and row[0] == "" and row[1] == "sec/op":
                break
            if not in_table or len(row) < 6 or row[0] == "geomean":
                continue
            rows += 1
            delta = row[5].strip()
            if delta.endswith("%") and not delta.startswith("-"):
                pct = float(delta.rstrip("%").lstrip("+"))
                if pct > threshold:
                    regressions.append((row[0], pct))
    if rows == 0:
        print("no sec/op benchmark rows found: benchstat output changed or nothing was compared")
        return 2
    for name, pct in regressions:
        print(f"REGRESSION {name}: +{pct:.1f}% time/op (threshold {threshold:.0f}%)")
    if regressions:
        return 1
    print(f"{rows} benchmarks compared, no significant time/op regression above {threshold:.0f}%")
    return 0


if __name__ == "__main__":
    sys.exit(main())
