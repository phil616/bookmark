#!/usr/bin/env python3
"""Check release metadata and build allowlisted source/native archives."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[1]
ROOT_FILES = (
    '.cnb.yml', '.dockerignore', '.editorconfig', '.env.example', '.gitignore',
    'CHANGELOG.md', 'LICENSE', 'Makefile', 'README.md', 'THIRD_PARTY_NOTICES.md',
    'Dockerfile', 'compose.yaml', 'config.example.json', 'go.mod', 'go.sum',
    'web/.dockerignore', 'web/.env.example', 'web/.env.e2e', 'web/Dockerfile',
    'web/nginx.conf', 'web/package.json', 'web/package-lock.json',
    'web/tsconfig.json', 'web/vite.config.ts', 'web/playwright.config.ts',
    'web/index.html',
)
SOURCE_DIRS = ('cmd', 'internal', 'docs', 'scripts', 'web/src', 'web/public', 'web/tests')
SKIP_PARTS = {'__pycache__', 'node_modules', 'test-results', 'playwright-report', '.git'}


def run(args, **kwargs):
    print('+ ' + ' '.join(args), flush=True)
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def output(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def check():
    match = re.search(r'const Version = "(\d+\.\d+\.\d+)"', (ROOT / 'internal/app/version.go').read_text())
    if not match:
        raise ValueError('application Version must be a release version such as 1.0.0')
    version = match.group(1)
    package = json.loads((ROOT / 'web/package.json').read_text())
    lock = json.loads((ROOT / 'web/package-lock.json').read_text())
    spec = json.loads((ROOT / 'docs/api/openapi.json').read_text())
    for label, actual in {
        'package.json': package['version'], 'package-lock.json': lock['version'],
        'lock root package': lock['packages']['']['version'], 'OpenAPI': spec['info']['version'],
    }.items():
        if actual != version:
            raise ValueError(f'{label} version {actual} does not match {version}')
    docker_version = re.search(r'org.opencontainers.image.version="([^"]+)"', (ROOT / 'Dockerfile').read_text())
    if not docker_version or docker_version.group(1) != version:
        raise ValueError('Docker image version does not match the application')
    web_docker_version = re.search(r'org.opencontainers.image.version="([^"]+)"', (ROOT / 'web/Dockerfile').read_text())
    if not web_docker_version or web_docker_version.group(1) != version:
        raise ValueError('frontend image version does not match the application')
    if package.get('license') != 'MIT' or 'Copyright (c) 2026 phil616' not in (ROOT / 'LICENSE').read_text():
        raise ValueError('project MIT license metadata is missing')
    if f'## v{version} ' not in (ROOT / 'CHANGELOG.md').read_text():
        raise ValueError('CHANGELOG.md is missing the release entry')
    if (ROOT / 'plan.md').exists():
        raise ValueError('obsolete plan.md must not be included')
    errors = []
    for path in [*ROOT.glob('*.md'), *(ROOT / 'docs').rglob('*.md')]:
        for target in re.findall(r'\[[^\]]*\]\(([^)]+)\)', path.read_text()):
            target = target.strip('<>').split('#', 1)[0]
            if not target or re.match(r'[a-zA-Z][a-zA-Z0-9+.-]*:', target):
                continue
            if not (path.parent / unquote(target)).exists():
                errors.append(f'{path.relative_to(ROOT)}: {target}')
    if errors:
        raise ValueError('broken documentation links:\n' + '\n'.join(errors))
    print(f'Release v{version}: metadata and local document links OK', flush=True)
    return version


def license_files(directory):
    return sorted(p for p in directory.iterdir() if p.is_file() and
                  any(key in p.name.lower() for key in ('license', 'licence', 'notice', 'copying', 'ofl', 'patents')))


def collect_licenses():
    sections = []
    lock = json.loads((ROOT / 'web/package-lock.json').read_text())
    overrides = {
        'node_modules/@ant-design/icons-svg': ('4.6.0', 'ant-design-icons.LICENSE'),
        'node_modules/is-mobile': ('5.0.0', 'is-mobile.LICENSE'),
    }
    for relative, meta in sorted(lock['packages'].items()):
        if not relative or meta.get('dev'):
            continue
        directory = ROOT / 'web' / relative
        if not directory.is_dir():
            if meta.get('optional'):
                continue
            raise ValueError(f'missing installed runtime dependency: {relative}; run npm ci')
        files = license_files(directory)
        if not files and relative in overrides:
            expected, filename = overrides[relative]
            if meta['version'] != expected:
                raise ValueError(f'refresh the upstream license override for {relative}')
            files = [ROOT / 'docs/licenses/upstream' / filename]
        if not files:
            raise ValueError(f'no license text for {relative}')
        name = relative.rsplit('node_modules/', 1)[-1]
        sections.append((f'npm: {name}@{meta["version"]} ({meta.get("license", "see license text")})', files))
    modules = output(['go', 'list', '-deps', '-f', '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}{{end}}', './cmd/server'])
    for line in sorted(set(modules.splitlines())):
        if not line:
            continue
        name, version, directory = line.split('|', 2)
        files = license_files(Path(directory))
        if not files:
            raise ValueError(f'no license text for Go module {name}')
        sections.append((f'Go module: {name}@{version}', files))
    sections.append((f'Go runtime and standard library: {output(["go", "env", "GOVERSION"])}', [Path(output(['go', 'env', 'GOROOT'])) / 'LICENSE']))
    text = ['Third-party license texts for the locked runtime dependencies.',
            'Generated by scripts/release.py --licenses; do not edit this file manually.', '']
    for heading, files in sections:
        text.extend(['=' * 72, heading, '=' * 72])
        for path in files:
            text.extend([f'--- {path.name} ---', path.read_text(errors='replace').strip(), ''])
    destination = ROOT / 'docs/licenses/THIRD_PARTY_LICENSES.txt'
    destination.write_text('\n'.join(text).rstrip() + '\n')
    print(f'Collected license texts for {len(sections)} runtime components', flush=True)


def source_files():
    files = {ROOT / name for name in ROOT_FILES}
    for directory in SOURCE_DIRS:
        files.update(p for p in (ROOT / directory).rglob('*') if p.is_file() and
                     not SKIP_PARTS.intersection(p.relative_to(ROOT).parts) and p.suffix != '.pyc')
    for path in files:
        if not path.is_file() or path.is_symlink():
            raise ValueError(f'non-regular or missing source file: {path}')
    return sorted(files)


def archive(destination, prefix, files):
    with tarfile.open(destination, 'w:gz') as bundle:
        for path, relative in files:
            bundle.add(path, arcname=f'{prefix}/{relative}', recursive=False)


def build(version, force):
    target_os = output(['go', 'env', 'GOHOSTOS'])
    target_arch = output(['go', 'env', 'GOHOSTARCH'])
    if target_os != 'linux':
        raise ValueError('this release target currently packages a native Linux build; use Docker elsewhere')
    dist = ROOT / 'dist'
    names = [f'online-bookmarks-v{version}-source.tar.gz', f'online-bookmarks-v{version}-{target_os}-{target_arch}.tar.gz', 'SHA256SUMS']
    for name in names:
        if (dist / name).exists() and not force:
            raise ValueError(f'dist/{name} already exists; use --force only to replace local unpublished packages')
    env = dict(os.environ, CGO_ENABLED='1', GOOS=target_os, GOARCH=target_arch)
    # Runtime dependency licenses are collected from the installed npm tree.
    run(['npm', '--prefix', 'web', 'ci'])
    collect_licenses()
    check()
    dist.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='online-bookmarks-release-') as temporary:
        stage = Path(temporary)
        native = stage / 'native'
        native.mkdir()
        run(['go', 'build', '-trimpath', '-ldflags=-s -w', '-o', str(native / 'online-bookmarks'), './cmd/server'], env=env)
        actual = output([str(native / 'online-bookmarks'), '--version'])
        if actual != f'online-bookmarks {version}':
            raise ValueError(f'built binary has unexpected version: {actual}')
        for name in ('LICENSE', 'CHANGELOG.md', 'THIRD_PARTY_NOTICES.md', '.env.example', 'config.example.json'):
            shutil.copy2(ROOT / name, native / name)
        shutil.copytree(ROOT / 'docs', native / 'docs')
        (native / 'README.md').write_text(f'''# online-bookmarks v{version}

Native {target_os}/{target_arch} build of the API server. The web frontend is
built and deployed separately.

```sh
./online-bookmarks --version
INITIAL_ADMIN_KEY=replace-with-at-least-16-characters ./online-bookmarks
```

The API listens on :8080 and reports readiness on /healthz; /api/v1, /mcp and
/healthz are the only routes it serves. Build the frontend from the source
release with `VITE_API_BASE_URL` set to this API's public origin, upload
`web/dist` to a static host, and set `ALLOWED_ORIGINS` to the frontend origin.
Frontend and API must share a site (same registrable domain) so the SameSite=Lax
session cookie is sent.

The binary uses CGO and dynamically links the build system's C library; see
BUILD_INFO.json. For portable deployment use the Docker instructions in the
source release.

Configuration, backup and migration: docs/deployment.md
Usage: docs/usage.md
License: LICENSE and THIRD_PARTY_NOTICES.md
''')
        (native / 'BUILD_INFO.json').write_text(json.dumps({
            'version': version, 'os': target_os, 'arch': target_arch,
            'go': output(['go', 'version']), 'node': output(['node', '--version']),
            'cgo_enabled': True, 'libc': dict(zip(('name', 'version'), platform.libc_ver())),
        }, indent=2) + '\n')
        source_prefix = names[0].removesuffix('.tar.gz')
        archive(stage / names[0], source_prefix, [(p, p.relative_to(ROOT)) for p in source_files()])
        native_prefix = names[1].removesuffix('.tar.gz')
        archive(stage / names[1], native_prefix, [(p, p.relative_to(native)) for p in sorted(native.rglob('*')) if p.is_file()])
        sums = ''.join(f'{hashlib.sha256((stage / name).read_bytes()).hexdigest()}  {name}\n' for name in names[:2])
        (stage / 'SHA256SUMS').write_text(sums)
        for name in names:
            os.replace(stage / name, dist / name)
    print('\nPrepared release assets:\n' + '\n'.join(str(dist / name) for name in names))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    group = parser.add_mutually_exclusive_group()
    group.add_argument('--check', action='store_true', help='check metadata and local Markdown links only')
    group.add_argument('--licenses', action='store_true', help='refresh third-party runtime license texts')
    parser.add_argument('--force', action='store_true', help='overwrite local, unpublished release assets')
    args = parser.parse_args()
    if args.licenses:
        collect_licenses()
    elif args.check:
        check()
    else:
        build(check(), args.force)


if __name__ == '__main__':
    main()
