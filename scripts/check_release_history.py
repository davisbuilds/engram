#!/usr/bin/env python3
"""Validate main push ancestry and complete unreleased commit categories."""

import argparse
import json
import re
import subprocess
import sys

CONVENTIONAL = re.compile(
    r"^(feat|fix|perf|docs|test|chore|build|ci|style|refactor|revert)"
    r"(\([^\r\n()]+\))?!?: \S.*$"
)


# SemVer 2.0.0 grammar: https://semver.org/#backusnaur-form-grammar-for-valid-semver-versions
_NUMERIC = r"(?:0|[1-9][0-9]*)"
_PRERELEASE = rf"(?:{_NUMERIC}|[0-9]*[A-Za-z-][0-9A-Za-z-]*)"
_BUILD = r"[0-9A-Za-z-]+"
SEMVER = re.compile(
    rf"{_NUMERIC}\.{_NUMERIC}\.{_NUMERIC}"
    rf"(?:-{_PRERELEASE}(?:\.{_PRERELEASE})*)?"
    rf"(?:\+{_BUILD}(?:\.{_BUILD})*)?"
)


def valid_subject(subject):
    return CONVENTIONAL.fullmatch(subject) is not None


def check_range(base, head, *, push=False, require_ancestor=False):
    for revision in (base, head):
        if not re.fullmatch(r"[0-9a-f]{40}", revision) or revision == "0" * 40:
            raise ValueError("release validation requires nonzero full commit SHAs")
        subprocess.run(
            ["git", "cat-file", "-e", f"{revision}^{{commit}}"],
            check=True, capture_output=True,
        )
    if push or require_ancestor:
        if push and base == head:
            raise ValueError("release validation requires a nonempty push range")
        subprocess.run(
            ["git", "merge-base", "--is-ancestor", base, head],
            check=True, capture_output=True,
        )
    # Actual merge commits carry no new consumer change; validate retained leaves.
    subjects = subprocess.check_output(
        ["git", "log", "--no-merges", "--format=%s", f"{base}..{head}"],
        text=True,
    ).splitlines()
    return [subject for subject in subjects if not valid_subject(subject)]


def release_boundary():
    with open("release-please-config.json", encoding="utf-8") as config_file:
        bootstrap = json.load(config_file)["bootstrap-sha"]
    with open(".release-please-manifest.json", encoding="utf-8") as manifest_file:
        version = json.load(manifest_file)["."]
    if not isinstance(version, str) or SEMVER.fullmatch(version) is None:
        raise ValueError("release manifest requires an application SemVer")
    if version != "0.0.0":
        tag = f"refs/tags/v{version}"
        result = subprocess.run(["git", "show-ref", "--verify", "--quiet", tag])
        if result.returncode == 0:
            return subprocess.check_output(
                ["git", "rev-parse", "--verify", f"{tag}^{{commit}}"], text=True,
            ).strip()
        if result.returncode != 1:
            result.check_returncode()
    # A release PR updates the manifest before its tag exists. Bootstrap is
    # conservative during that gap and never fabricates a historical release.
    return bootstrap


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base_sha")
    parser.add_argument("head_sha")
    args = parser.parse_args()
    try:
        invalid = check_range(args.base_sha, args.head_sha, push=True)
        # Re-check all unreleased subjects so a later successful push cannot
        # forget classification that failed on an earlier main push.
        invalid += check_range(release_boundary(), args.head_sha, require_ancestor=True)
        invalid = list(dict.fromkeys(invalid))
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
    if invalid:
        for subject in invalid:
            print(f"Invalid Conventional Commit subject: {subject}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
