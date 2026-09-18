import Base
import ./follow_up.bend as FollowUp
import ./dispatch.bend as Dispatch

law outbound_requires_triage:
  for state: FollowUp.ThreadState
  {FollowUp.apply_outbound(state) == FollowUp.New{} : FollowUp.ThreadState}

law snoozed_is_silent:
  for recent: Bool
  for state: FollowUp.ThreadState
  for fires: Nat
  for overdue: Bool
  {FollowUp.can_nudge(True{}, recent, state, fires, overdue) == False{} : Bool}

law unclassified_is_silent:
  for snoozed: Bool
  for recent: Bool
  for fires: Nat
  for overdue: Bool
  {FollowUp.can_nudge(snoozed, recent, FollowUp.New{}, fires, overdue) == False{} : Bool}

law done_is_silent:
  for snoozed: Bool
  for recent: Bool
  for fires: Nat
  for overdue: Bool
  {FollowUp.can_nudge(snoozed, recent, FollowUp.Done{}, fires, overdue) == False{} : Bool}

law overdue_request_can_nudge:
  for fires: Nat
  {FollowUp.can_nudge(False{}, False{}, FollowUp.WaitingOnThem{}, fires, True{}) == True{} : Bool}

law cooldown_is_silent:
  for snoozed: Bool
  for state: FollowUp.ThreadState
  for fires: Nat
  for overdue: Bool
  {FollowUp.can_nudge(snoozed, True{}, state, fires, overdue) == False{} : Bool}

law reply_limit_is_silent:
  for extra: Nat
  for overdue: Bool
  {FollowUp.can_nudge(False{}, False{}, FollowUp.NeedsReply{}, 1n+1n+extra, overdue) == False{} : Bool}

law first_reply_can_nudge:
  {FollowUp.can_nudge(False{}, False{}, FollowUp.NeedsReply{}, 0n, True{}) == True{} : Bool}

law second_reply_can_nudge:
  {FollowUp.can_nudge(False{}, False{}, FollowUp.NeedsReply{}, 1n, True{}) == True{} : Bool}

law quiet_batch_is_silent:
  for fired: Nat
  for candidates: Nat
  {Dispatch.batch_count(True{}, fired, candidates) == 0n : Nat}

law exhausted_batch_is_silent:
  for extra: Nat
  for candidates: Nat
  {Dispatch.batch_count(False{}, 5n+extra, candidates) == 0n : Nat}

law batch_within_allowance:
  for +fired: Nat
  for candidates: Nat
  {Nat.is_le(Dispatch.batch_count(False{}, fired, candidates), Nat.sub(5n, fired)) == True{} : Bool}

law full_allowance_is_used:
  {Dispatch.batch_count(False{}, 0n, 10n) == 5n : Nat}

law failed_delivery_stays_stopped:
  for +confirmed: Nat
  for succeeded: Bool
  {Dispatch.delivery_step(True{}, Dispatch.Failed{confirmed}, succeeded) == Dispatch.Failed{confirmed} : Dispatch.DeliveryState}

law failed_delivery_is_not_counted:
  for +confirmed: Nat
  {Dispatch.delivery_step(True{}, Dispatch.Ready{confirmed}, False{}) == Dispatch.Failed{confirmed} : Dispatch.DeliveryState}

law successful_delivery_is_counted:
  for +confirmed: Nat
  {Dispatch.delivery_step(True{}, Dispatch.Ready{confirmed}, True{}) == Dispatch.Ready{1n+confirmed} : Dispatch.DeliveryState}

law skipped_delivery_is_unchanged:
  for +state: Dispatch.DeliveryState
  for succeeded: Bool
  {Dispatch.delivery_step(False{}, state, succeeded) == state : Dispatch.DeliveryState}
