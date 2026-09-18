import Base

type ThreadState is Data:
  New{}
  NeedsReply{}
  WaitingOnThem{}
  FYI{}
  Noise{}
  Done{}

def apply_outbound(state: ThreadState) -> ThreadState:
  New{}

def reply_limit(fires: Nat, overdue: Bool) -> Bool:
  match fires:
    case 0n:
      overdue
    case 1n+previous:
      match previous:
        case 0n:
          overdue
        case 1n+older:
          False{}

def can_nudge(snoozed: Bool, recent: Bool, state: ThreadState, fires: Nat, overdue: Bool) -> Bool:
  match snoozed:
    case True{}:
      False{}
    case False{}:
      match recent:
        case True{}:
          False{}
        case False{}:
          match state:
            case New{}:
              False{}
            case NeedsReply{}:
              reply_limit(fires, overdue)
            case WaitingOnThem{}:
              overdue
            case FYI{}:
              False{}
            case Noise{}:
              False{}
            case Done{}:
              False{}
