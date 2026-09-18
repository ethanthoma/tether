#! /usr/bin/env nix-shell
#! nix-shell -i bash -p bun clang curl gnutar gzip gnused gnugrep coreutils go

set -euo pipefail

experiment_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
trial_dir=$(mktemp -d)
trap 'rm -rf -- "$trial_dir"' EXIT

curl --fail --silent --show-error --location --connect-timeout 10 --max-time 120 \
	https://bend-lang.com/dl/2.0.5.tar.gz --output "$trial_dir/bend.tar.gz"
printf '%s  %s\n' 4db70e77ce1b1027f1d0e15dee025921fa794a9b415add4350ec7c64acf2775b \
	"$trial_dir/bend.tar.gz" | sha256sum --check
mkdir "$trial_dir/toolchain"
tar --extract --gzip --file "$trial_dir/bend.tar.gz" --directory "$trial_dir/toolchain"
bend_main="$trial_dir/toolchain/bend2/main.ts"

bun "$bend_main" --version
bun "$bend_main" "$experiment_dir/PROOF.bend"
bun "$bend_main" "$experiment_dir/shadow.bend" -o "$trial_dir/tether-bend-shadow"

bun "$bend_main" "$experiment_dir/dispatch_shadow.bend" -o "$trial_dir/tether-bend-dispatch"

(
	cd "$experiment_dir/../.."
	TETHER_BEND_DISPATCH="$trial_dir/tether-bend-dispatch" TETHER_BEND_SHADOW="$trial_dir/tether-bend-shadow" BEND_MAIN="$bend_main" \
		go test -mod=vendor -tags bend -run '^TestBend(FollowUp|Dispatch|Delivery|NativeShadow|NativeDispatch)Agreement$' -count=1 .
)

mkdir "$trial_dir/mutant"
cp "$experiment_dir/LAWS.bend" "$experiment_dir/PROOF.bend" "$trial_dir/mutant/"
for mutation in outbound snoozed unclassified cooldown reply_limit quiet_hours daily_cap remaining_allowance resume_failed count_failed drop_success; do
	cp "$experiment_dir/follow_up.bend" "$trial_dir/mutant/follow_up.bend"
	cp "$experiment_dir/dispatch.bend" "$trial_dir/mutant/dispatch.bend"
	case "$mutation" in
	outbound)
		sed -i '/^def apply_outbound/{n;s/New{}/WaitingOnThem{}/;}' "$trial_dir/mutant/follow_up.bend"
		expected_law=outbound_requires_triage
		;;
	snoozed)
		sed -i '/case True{}:/{n;s/False{}/True{}/;}' "$trial_dir/mutant/follow_up.bend"
		expected_law=snoozed_is_silent
		;;
	unclassified)
		sed -i '/case New{}:/{n;s/False{}/overdue/;}' "$trial_dir/mutant/follow_up.bend"
		expected_law=unclassified_is_silent
		;;
	cooldown)
		sed -i '/match recent:/{n;n;s/False{}/overdue/;}' "$trial_dir/mutant/follow_up.bend"
		expected_law=cooldown_is_silent
		;;
	reply_limit)
		sed -i '/case 1n+older:/{n;s/False{}/overdue/;}' "$trial_dir/mutant/follow_up.bend"
		expected_law=reply_limit_is_silent
		;;
	quiet_hours)
		sed -i '/case True{}:/{n;s/0n/1n/;}' "$trial_dir/mutant/dispatch.bend"
		expected_law=quiet_batch_is_silent
		;;
	daily_cap)
		sed -i '/case 5n+extra:/{n;s/0n/1n/;}' "$trial_dir/mutant/dispatch.bend"
		expected_law=exhausted_batch_is_silent
		;;
	remaining_allowance)
		sed -i '/case 1n:/{n;s/4n/5n/;}' "$trial_dir/mutant/dispatch.bend"
		expected_law=batch_within_allowance
		;;
	resume_failed)
		sed -i '/case Failed{confirmed}:/{n;s/Failed/Ready/;}' "$trial_dir/mutant/dispatch.bend"
		expected_law=failed_delivery_stays_stopped
		;;
	count_failed)
		sed -i '/match succeeded:/{n;n;s/Failed{confirmed}/Failed{1n+confirmed}/;}' "$trial_dir/mutant/dispatch.bend"
		expected_law=failed_delivery_is_not_counted
		;;
	drop_success)
		sed -i 's/Ready{1n+confirmed}/Ready{confirmed}/' "$trial_dir/mutant/dispatch.bend"
		expected_law=successful_delivery_is_counted
		;;
	esac
	if bun "$bend_main" "$trial_dir/mutant/PROOF.bend" >"$trial_dir/rejection.txt" 2>&1; then
		printf 'FAIL: %s regression passed proof checking\n' "$mutation" >&2
		exit 1
	fi
	if ! grep --fixed-strings --quiet "Location: LAWS.$expected_law" "$trial_dir/rejection.txt"; then
		cat "$trial_dir/rejection.txt" >&2
		printf 'FAIL: %s failed for an unexpected reason\n' "$mutation" >&2
		exit 1
	fi
	printf 'Rejected %s regression at %s\n' "$mutation" "$expected_law"
done
