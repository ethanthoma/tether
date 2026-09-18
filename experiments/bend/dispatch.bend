import Base

def remaining(fired: Nat) -> Nat:
  match fired:
    case 0n:
      5n
    case 1n:
      4n
    case 2n:
      3n
    case 3n:
      2n
    case 4n:
      1n
    case 5n+extra:
      0n

def batch_count(quiet: Bool, fired: Nat, candidates: Nat) -> Nat:
  match quiet:
    case True{}:
      0n
    case False{}:
      Nat.min(remaining(fired), candidates)

type DeliveryState is Data:
  Ready{confirmed: Nat}
  Failed{confirmed: Nat}

def delivery_step(allowed: Bool, state: DeliveryState, succeeded: Bool) -> DeliveryState:
  match allowed:
    case False{}:
      state
    case True{}:
      match state:
        case Failed{confirmed}:
          Failed{confirmed}
        case Ready{confirmed}:
          match succeeded:
            case False{}:
              Failed{confirmed}
            case True{}:
              Ready{1n+confirmed}

def deliver(quiet: Bool, fired: Nat, candidates: Nat, +successes: Nat) -> DeliveryState:
  +planned = batch_count(quiet, fired, candidates)
  first = delivery_step(Nat.is_gt(planned, 0n), Ready{0n}, Nat.is_gt(successes, 0n))
  second = delivery_step(Nat.is_gt(planned, 1n), first, Nat.is_gt(successes, 1n))
  third = delivery_step(Nat.is_gt(planned, 2n), second, Nat.is_gt(successes, 2n))
  fourth = delivery_step(Nat.is_gt(planned, 3n), third, Nat.is_gt(successes, 3n))
  delivery_step(Nat.is_gt(planned, 4n), fourth, Nat.is_gt(successes, 4n))
