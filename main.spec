# -*- mode: python ; coding: utf-8 -*-

import sys

sys.path.append("./.venv/lib/python3.13/site-packages")

block_cipher = None

a = Analysis(
    ['main.py'],
    pathex=['.'],
    paths=["./.venv/lib/python3.13/site-packages"],
    binaries=[],
    datas=[],
    hiddenimports=[],
    hookspath=[],
    hooksconfig={},
    runtime_hooks=[],
    excludes=[],
    noarchive=False,
)

pyz = PYZ(a.pure, a.zipped_data, cipher=block_cipher)

exe = EXE(
    pyz,
    a.scripts,
    a.binaries,
    a.datas,
    [],
    name='static',
    debug=False,
    bootloader_ignore_signals=False,
    strip=False,
    upx=True,
    upx_exclude=[],
    runtime_tmpdir=None,
    console=True,
)

