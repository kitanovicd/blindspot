// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

import {Vault} from "../src/Vault.sol";

// No external test dependencies are needed for this demo.
contract VaultTest {
    Vault internal vault;

    function setUp() public { vault = new Vault(); }

    function test_DepositUpdatesBalance() public {
        vault.deposit(100);
        require(vault.balances(address(this)) == 100, "balance");
        require(vault.totalDeposits() == 100, "total");
    }

    function test_PartialWithdrawal() public {
        vault.deposit(100);
        vault.withdraw(40);
        require(vault.balances(address(this)) == 60, "remaining balance");
        require(vault.totalDeposits() == 60, "remaining total");
    }

    function test_OverdraftReverts() public {
        vault.deposit(10);
        (bool ok,) = address(vault).call(abi.encodeCall(vault.withdraw, (11)));
        require(!ok, "overdraft must revert");
    }
}
