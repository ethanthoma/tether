{ pkgs, ... }:
let
  bend-shadow = import ./bend-shadow.nix { inherit pkgs; };
  tether-bin = pkgs.buildGoModule {
    pname = "tether";
    version = "0.1.0";
    src = ./.;
    vendorHash = null;
  };
  tether = pkgs.writeShellScriptBin "tether" ''
    export TETHER_BEND_SHADOW=${bend-shadow}/bin/tether-bend-shadow
    export TETHER_BEND_DISPATCH=${bend-shadow}/bin/tether-bend-dispatch
    set -a
    [ -f /var/lib/tether.env ] && . /var/lib/tether.env
    set +a
    exec ${tether-bin}/bin/tether "$@"
  '';
  serviceDefaults = {
    Type = "oneshot";
    User = "ethoma";
    Group = "users";
  };
in
{
  environment.systemPackages = [ tether ];

  systemd.tmpfiles.rules = [ "d /var/lib/tether 0700 ethoma users -" ];

  # Discord delivers slash commands as signed POSTs, so the endpoint must be
  # reachable from the internet. Merges into the tunnel declared alongside llama-server.
  services.cloudflared.tunnels."93c151d8-b148-4fb8-b256-e1a67e45c601".ingress = {
    "tether.gaugenumerics.com" = "http://localhost:8082";
  };

  systemd.services.tether-bot = {
    description = "tether discord bot";
    wantedBy = [ "multi-user.target" ];
    wants = [ "network-online.target" ];
    after = [ "network-online.target" ];
    unitConfig.ConditionPathExists = "/var/lib/tether.env";
    serviceConfig = {
      ExecStart = "${tether}/bin/tether bot";
      User = "ethoma";
      Group = "users";
      Restart = "always";
      RestartSec = 15;
    };
  };

  systemd.services.tether-pulse = {
    description = "tether pulse: sync + triage + nudge";
    unitConfig.ConditionPathExists = "/var/lib/tether.env";
    serviceConfig = serviceDefaults // {
      ExecStart = "${tether}/bin/tether pulse";
    };
  };
  systemd.timers.tether-pulse = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "*:0/15";
      RandomizedDelaySec = 60;
    };
  };

  systemd.services.tether-bend-shadow = {
    description = "tether read-only Bend policy comparisons";
    unitConfig.ConditionPathExists = "/var/lib/tether/lock";
    environment.TETHER_BEND_SHADOW = "${bend-shadow}/bin/tether-bend-shadow";
    environment.TETHER_BEND_DISPATCH = "${bend-shadow}/bin/tether-bend-dispatch";
    serviceConfig = serviceDefaults // {
      ExecStart = [
        "${tether-bin}/bin/tether shadow"
        "${tether-bin}/bin/tether shadow-dispatch"
      ];
      TimeoutStartSec = 10;
      MemoryMax = "128M";
      CPUQuota = "20%";
      Nice = 10;
      NoNewPrivileges = true;
      PrivateNetwork = true;
      PrivateTmp = true;
      ProtectSystem = "strict";
      ReadWritePaths = [ "/var/lib/tether/lock" ];
    };
  };
  systemd.timers.tether-bend-shadow = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnBootSec = "2min";
      OnUnitActiveSec = "15min";
    };
  };

  systemd.services.tether-digest = {
    description = "tether morning digest";
    unitConfig.ConditionPathExists = "/var/lib/tether.env";
    serviceConfig = serviceDefaults // {
      ExecStart = "${tether}/bin/tether digest";
    };
  };
  systemd.timers.tether-digest = {
    wantedBy = [ "timers.target" ];
    timerConfig = {
      OnCalendar = "07:00";
      Persistent = true;
    };
  };
}
