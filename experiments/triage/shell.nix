{
  pkgs ? import <nixpkgs> { },
}:
pkgs.mkShell {
  packages = with pkgs; [
    python3
    uv
    ruff
  ];
  LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath [
    pkgs.stdenv.cc.cc.lib
    pkgs.zlib
  ];
  HF_HUB_DISABLE_TELEMETRY = "1";
  TOKENIZERS_PARALLELISM = "false";
  OMP_NUM_THREADS = "4";
  OPENBLAS_NUM_THREADS = "4";
}
