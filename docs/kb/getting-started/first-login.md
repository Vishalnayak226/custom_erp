---
title: Your first sign-in
section: Getting Started
order: 2
summary: Sign in, understand what your role can see, and find your way around the main screens.
audience: everyone
last_verified: 2026-08-12
screens: [profile, configuration]
---

# Your first sign-in

Your administrator creates your account and tells you your username. The first
time you sign in you will be asked to change the password you were given.

## Signing in

1. Open the address your administrator gave you.
2. Enter your username and password.
3. If your account has two-factor authentication enabled, enter the six-digit
   code from your authenticator app.

> [!NOTE]
> Five failed sign-in attempts in a minute are refused for a short cooling-off
> period. This is a rate limit, not a lockout - wait a minute and try again.

If you have genuinely forgotten your password, use **Forgot password** on the
sign-in screen rather than asking someone to reset it for you.

## What you can see depends on your role

The application shows you only what your role is allowed to open. If a colleague
can see a screen you cannot, that is a permission difference, not a fault.

| Role | Typically works in |
|---|---|
| Cashier | Point of sale, customer lookup |
| Store Manager | Store operations, stock, orders, local reports |
| Super Admin | Everything, including users, roles and system settings |

Ask an administrator to change your role under **Admin » Roles** if you need
access you do not have. Nothing in the application lets you grant it to
yourself, and that is deliberate.

## Finding your way around

- The left sidebar is your module list. It only lists modules your tenant has
  bought and your role can open.
- The **?** button in the header opens help for the screen you are on.
- Every error dialog shows a code such as `GLOBAL-0001`. Quote it when you ask
  for help - it identifies the exact condition, and the
  [error code reference](error-codes.md) explains what to do about it.

## Next

Read [Your first order, end to end](first-order.md) to follow a single order
from placement through to invoice.
