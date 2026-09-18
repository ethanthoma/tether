import Base
import ./follow_up.bend as Policy

def bit(value: Bool) -> String:
  match value:
    case True{}:
      "1"
    case False{}:
      "0"

def counts(+snoozed: Bool, +recent: Bool, +state: Policy.ThreadState, +overdue: Bool) -> String:
  first = bit(Policy.can_nudge(snoozed, recent, state, 0n, overdue))
  second = bit(Policy.can_nudge(snoozed, recent, state, 1n, overdue))
  last = bit(Policy.can_nudge(snoozed, recent, state, 2n, overdue))
  first ++ second ++ last

def states(+state: Policy.ThreadState) -> String:
  first = counts(False{}, False{}, state, False{}) ++ counts(True{}, False{}, state, False{})
  second = counts(False{}, True{}, state, False{}) ++ counts(True{}, True{}, state, False{})
  third = counts(False{}, False{}, state, True{}) ++ counts(True{}, False{}, state, True{})
  fourth = counts(False{}, True{}, state, True{}) ++ counts(True{}, True{}, state, True{})
  first ++ second ++ third ++ fourth

def main() -> IO(Unit):
  first = states(Policy.New{}) ++ states(Policy.NeedsReply{})
  second = states(Policy.WaitingOnThem{}) ++ states(Policy.FYI{})
  third = states(Policy.Noise{}) ++ states(Policy.Done{})
  IO.print("tether-bend-shadow-v1:" ++ first ++ second ++ third)
