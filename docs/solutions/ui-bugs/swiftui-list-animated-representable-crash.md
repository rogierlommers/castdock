---
title: Avoid animated insertion of dynamically sized UIKit views in SwiftUI lists
date: 2026-11-12
category: ui-bugs
module: iOS show-note rendering
problem_type: ui_bug
component: frontend
symptoms:
  - Expanding rich show notes crashes in SwiftUICore StackLayout.makeChildren
  - The crash occurs for larger show notes but not every episode
root_cause: async_timing
resolution_type: code_fix
severity: high
tags: [swiftui, uiviewrepresentable, uitextview, list, animation]
---

# Avoid animated insertion of dynamically sized UIKit views in SwiftUI lists

## Problem

Opening some rich show notes crashed with `EXC_BAD_ACCESS` in
`SwiftUICore.StackLayout.makeChildren`. Shorter show notes often worked, which
initially made the HTML importer look responsible.

## Symptoms

- The backtrace ended in SwiftUI stack layout and `AnimatableFrameAttribute`,
  not Foundation's HTML importer.
- The crash occurred while inserting a non-scrolling, dynamically measured
  `UITextView` into a `List` with an opacity transition.

## What Didn't Work

Build and decode tests did not exercise the animated SwiftUI layout path. The
publisher HTML was content-dependent, but sanitizing it further did not address
the immediate crash site in SwiftUI layout.

## Solution

Treat rich show-note visibility as a non-animated structural change. Toggle the
state directly and do not attach a transition to either the UIKit representable
or its plain-text fallback. Lightweight row expansion can remain animated.

## Why This Works

SwiftUI no longer has to insert and measure a dynamically sized UIKit view while
simultaneously maintaining animated child geometry for the surrounding list
stack. The view still computes its intrinsic height, but outside the problematic
animation transaction.

## Prevention

- Use crash frames to distinguish content parsing from layout failures.
- Avoid explicit transitions when conditionally inserting dynamically sized
  `UIViewRepresentable` content into a `List`.
- Manually verify long and short content because unit tests do not execute this
  layout path.

## Related Issues

- `clients/ios/CastDock/HomeView.swift`
