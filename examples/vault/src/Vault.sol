// SPDX-License-Identifier: MIT
pragma solidity ^0.8.28;

/// A deliberately small example with an incompletely tested withdrawal boundary.
contract Vault {
    mapping(address => uint256) public balances;
    uint256 public totalDeposits;

    function deposit(uint256 amount) external {
        require(amount > 0, "zero deposit");
        balances[msg.sender] = balances[msg.sender] + amount;
        totalDeposits = totalDeposits + amount;
    }

    function withdraw(uint256 amount) external {
        require(balances[msg.sender] >= amount, "insufficient balance");
        balances[msg.sender] = balances[msg.sender] - amount;
        totalDeposits = totalDeposits - amount;
    }

    // Deliberately not exercised by the example tests.
    function previewFee(uint256 amount) external pure returns (uint256) {
        return amount / 100;
    }
}
