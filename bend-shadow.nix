{ pkgs }:
let
  compiler = pkgs.fetchurl {
    url = "https://bend-lang.com/dl/2.0.5.tar.gz";
    sha256 = "4db70e77ce1b1027f1d0e15dee025921fa794a9b415add4350ec7c64acf2775b";
  };
in
pkgs.runCommand "tether-bend-shadow-2.0.5"
  {
    nativeBuildInputs = [
      pkgs.bun
      pkgs.clang
    ];
  }
  ''
    mkdir compiler
    tar -xzf ${compiler} -C compiler
    cp -r ${./experiments/bend} policy
    chmod -R u+w policy
    bun compiler/bend2/main.ts policy/PROOF.bend
    mkdir -p $out/bin
    bun compiler/bend2/main.ts policy/shadow.bend -o $out/bin/tether-bend-shadow
    $out/bin/tether-bend-shadow > table
    test "$(wc -c < table)" -eq 167
    grep -Eq '^tether-bend-shadow-v1:[01]{144}$' table
  ''
