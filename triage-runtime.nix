{
  pkgs ? import <nixpkgs> { },
  runtimeSource,
  pythonExecutable,
  pythonVersion ? "3.14",
}:
let
  pythonRoot = builtins.storePath (builtins.dirOf (builtins.dirOf pythonExecutable));
  python = "${pythonRoot}/bin/${builtins.baseNameOf pythonExecutable}";
  sitePackages = builtins.path {
    path = builtins.toPath "${runtimeSource}/lib/python${pythonVersion}/site-packages";
    name = "tether-triage-site-packages";
  };
in
pkgs.runCommand "tether-triage-runtime-v2"
  {
    nativeBuildInputs = [ pkgs.makeWrapper ];
  }
  ''
    mkdir -p "$out/site-packages" "$out/scripts" "$out/bin"
    cp -a ${sitePackages}/. "$out/site-packages/"
    cp ${./experiments/triage/train.py} "$out/scripts/train.py"
    cp ${./experiments/triage/shadow.py} "$out/scripts/shadow.py"
    makeWrapper ${python} "$out/bin/tether-triage-shadow" \
      --add-flags "$out/scripts/shadow.py" \
      --set PYTHONPATH "$out/site-packages:$out/scripts" \
      --set PYTHONNOUSERSITE 1 \
      --set PYTHONDONTWRITEBYTECODE 1 \
      --set LD_LIBRARY_PATH ${
        pkgs.lib.makeLibraryPath [
          pkgs.stdenv.cc.cc.lib
          pkgs.zlib
        ]
      } \
      --set HF_HUB_OFFLINE 1 \
      --set TRANSFORMERS_OFFLINE 1 \
      --set HF_HUB_DISABLE_TELEMETRY 1 \
      --set TOKENIZERS_PARALLELISM false \
      --set OMP_NUM_THREADS 4 \
      --set OPENBLAS_NUM_THREADS 4
  ''
