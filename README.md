# Gou Framework

[![Unit-Test](https://github.com/YaoApp/gou/actions/workflows/unit-test.yml/badge.svg)](https://github.com/YaoApp/gou/actions/workflows/unit-test.yml)
[![codecov](https://codecov.io/gh/YaoApp/gou/branch/main/graph/badge.svg?token=0Y9nhoBud9)](https://codecov.io/gh/YaoApp/gou)
[![Go Report Card](https://goreportcard.com/badge/github.com/yaoapp/gou)](https://goreportcard.com/report/github.com/yaoapp/gou)
[![Go Reference](https://pkg.go.dev/badge/github.com/yaoapp/gou.svg)](https://pkg.go.dev/github.com/yaoapp/gou)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FYaoApp%2Fgou.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FYaoApp%2Fgou?ref=badge_shield)

App engine framework

Gou 来自易经姤卦。《象》曰: 天下有风，姤。后以施命诰四方。

**Discord:** https://discord.gg/MJMQCJ2Q

**Documentation:** https://yaoapps.com/en-US/doc


第 0 步：创建备份（强烈建议！）
git branch backup-before-rebase

第 1 步：获取上游仓库的最新更新
git fetch upstream

第 2 步：开始变基
# 假设你要同步的分支是 main
git rebase upstream/main

第 3 步：解决冲突（如果出现）
git rebase --continue

如果你搞砸了或者想放弃 git rebase --abort

第 4 步：更新你的 Fork 仓库
git push origin main --force-with-lease

# 1. 确保有 upstream (只需设置一次)
git remote add upstream <官方仓库URL>

# 2. 获取官方最新代码
git fetch upstream

# 3. 将官方更新变基到你的分支上 (可能会有冲突需要解决)
git rebase upstream/main

# 4. 强制推送到你自己的 Fork 仓库，更新它
git push origin main --force-with-lease